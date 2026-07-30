package demo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/protect"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
)

func (service *Service) Monitor(ctx context.Context) {
	if !service.options.Enabled || service.runner == nil || service.store == nil {
		return
	}
	_ = service.MonitorOnce(ctx)
	ticker := time.NewTicker(service.options.MonitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = service.MonitorOnce(ctx)
		}
	}
}

func (service *Service) MonitorOnce(ctx context.Context) error {
	service.controlMu.Lock()
	defer service.controlMu.Unlock()

	run, err := service.store.GetActiveDemoRun(ctx)
	if errors.Is(err, storage.ErrDemoRunNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	now := service.options.Now().UTC()
	if now.Sub(run.RequestedAt) >= service.options.RunTimeout {
		if run.Status == model.DemoRunWaitingApproval {
			if err := service.denyTimedOutApproval(ctx, &run); err != nil {
				return err
			}
		}
		if _, err := service.runner.Cancel(ctx, run.RunID); err != nil && !errors.Is(err, ErrRunnerNotFound) {
			return err
		}
		timedOut := run
		timedOut.Outcome = model.DemoOutcomeFailed
		timedOut.StatusReasonCode = "run_timeout"
		timedOut.CompletedAt = &now
		if run.Status == model.DemoRunWaitingApproval {
			timedOut.Status = model.DemoRunExpired
		} else {
			timedOut.Status = model.DemoRunInterrupted
		}
		_, _, err = service.persist(ctx, run, timedOut, "run.finished")
		return err
	}
	snapshot, err := service.runner.Get(ctx, run.RunID)
	if err != nil {
		if errors.Is(err, ErrRunnerContract) {
			_, persistErr := service.interruptForRunnerContract(ctx, run)
			return persistErr
		}
		lastSeen := run.RequestedAt
		if run.LastHeartbeatAt != nil {
			lastSeen = *run.LastHeartbeatAt
		}
		if now.Sub(lastSeen) < service.options.RunnerLostAfter {
			return err
		}
		interrupted := run
		interrupted.Status = model.DemoRunInterrupted
		interrupted.Outcome = model.DemoOutcomeFailed
		interrupted.StatusReasonCode = "runner_run_lost"
		interrupted.CompletedAt = &now
		_, _, persistErr := service.persist(ctx, run, interrupted, "run.finished")
		return persistErr
	}
	err = service.monitorSnapshot(ctx, run, snapshot)
	if !errors.Is(err, ErrRunnerContract) {
		return err
	}
	_, persistErr := service.interruptForRunnerContract(ctx, run)
	return persistErr
}

func (service *Service) denyTimedOutApproval(ctx context.Context, run *model.DemoRun) error {
	if service.approvals == nil || run.Approval == nil || run.Approval.SessionID != run.SessionID ||
		!strings.EqualFold(run.Approval.Status, "pending") {
		return ErrStateChanged
	}
	resolve := func(ticketID string) error {
		_, err := service.approvals.ResolveApproval(ctx, ticketID, "deny", model.ConfirmedActionRequest{
			Confirmed: true, Note: "Demo timeout: deny the pending simulated action before cancellation",
		})
		return err
	}
	if err := resolve(run.Approval.TicketID); err != nil {
		if !errors.Is(err, protect.ErrNotFound) {
			return err
		}
		pending, lookupErr := service.approvals.PendingApprovalForSession(ctx, run.SessionID)
		if errors.Is(lookupErr, protect.ErrNotFound) {
			return nil
		}
		if lookupErr != nil || pending.SessionID != run.SessionID {
			return errors.Join(lookupErr, ErrStateChanged)
		}
		if err := resolve(pending.ID); err != nil {
			return err
		}
		run.Approval = projectApproval(pending)
	}
	approval := *run.Approval
	approval.MatchedRules = append([]string(nil), run.Approval.MatchedRules...)
	approval.Status = "denied"
	run.Approval = &approval
	return nil
}

func (service *Service) monitorSnapshot(ctx context.Context, run model.DemoRun, snapshot RunnerSnapshot) error {
	pendingApproval := (*model.Approval)(nil)
	if service.approvals != nil {
		approval, err := service.approvals.PendingApprovalForSession(ctx, run.SessionID)
		switch {
		case err == nil:
			pendingApproval = &approval
		case errors.Is(err, protect.ErrNotFound):
		default:
			// Missing, ambiguous, or unavailable AgentGuard data is not exact
			// correlation evidence. Runner progress remains independently usable.
		}
	}
	_, err := service.applyRunnerSnapshotWithApproval(ctx, run, snapshot, pendingApproval)
	return err
}

func (service *Service) applyRunnerSnapshot(ctx context.Context, run model.DemoRun, snapshot RunnerSnapshot) (model.DemoRun, error) {
	return service.applyRunnerSnapshotWithApproval(ctx, run, snapshot, nil)
}

func (service *Service) applyRunnerSnapshotWithApproval(ctx context.Context, run model.DemoRun, snapshot RunnerSnapshot, pending *model.Approval) (model.DemoRun, error) {
	if err := validateRunnerSnapshot(run, snapshot); err != nil {
		return model.DemoRun{}, err
	}
	candidate := run
	candidate.LastHeartbeatAt = timePointer(snapshot.HeartbeatAt.UTC())
	if snapshot.StartedAt != nil {
		candidate.StartedAt = timePointer(snapshot.StartedAt.UTC())
	}
	if snapshot.CompletedAt != nil {
		candidate.CompletedAt = timePointer(snapshot.CompletedAt.UTC())
	}
	candidate.TraceID = snapshot.TraceID
	candidate.RootSpanID = snapshot.RootSpanID
	candidate.CurrentStep = snapshot.CurrentStep
	candidate.CompletedSteps = snapshot.CompletedSteps
	candidate.TotalSteps = snapshot.TotalSteps
	candidate.Outcome = snapshot.Outcome
	candidate.ErrorCode = boundedRunnerErrorCode(snapshot.ErrorCode)
	if candidate.ErrorCode != "" {
		candidate.ErrorSummary = "The deterministic Demo Runner reported an execution error."
	} else {
		candidate.ErrorSummary = ""
	}
	candidate.Status = mappedRunnerStatus(snapshot.Status)
	if run.Status == model.DemoRunWaitingApproval && candidate.Status == model.DemoRunRunning && pending == nil &&
		snapshot.CurrentStep == run.CurrentStep && snapshot.CompletedSteps == run.CompletedSteps {
		candidate.Status = model.DemoRunWaitingApproval
	}
	if pending != nil {
		candidate.Approval = projectApproval(*pending)
		if candidate.Status == model.DemoRunRunning {
			candidate.Status = model.DemoRunWaitingApproval
		}
	} else if candidate.Approval != nil && terminal(candidate.Status) {
		switch candidate.Outcome {
		case model.DemoOutcomeApproved:
			candidate.Approval.Status = "approved"
		case model.DemoOutcomeDenied:
			candidate.Approval.Status = "denied"
		}
	}
	if terminal(candidate.Status) && candidate.CompletedAt == nil {
		candidate.CompletedAt = timePointer(service.options.Now().UTC())
	}
	eventType := runnerEventType(run, candidate)
	if !demoRunChanged(run, candidate) {
		return run, nil
	}
	returnValue, _, err := service.persist(ctx, run, candidate, eventType)
	return returnValue, err
}

func validateRunnerSnapshot(run model.DemoRun, snapshot RunnerSnapshot) error {
	mappedStatus := mappedRunnerStatus(snapshot.Status)
	if snapshot.RunID != run.RunID || snapshot.Scenario != run.Scenario || snapshot.DelayMS != run.DelayMS ||
		snapshot.TaskID != run.TaskID || snapshot.SessionID != run.SessionID || snapshot.RequestID != run.RequestID ||
		snapshot.TotalSteps != run.TotalSteps || snapshot.CompletedSteps < 0 || snapshot.CompletedSteps > snapshot.TotalSteps ||
		snapshot.CompletedSteps < run.CompletedSteps || snapshot.HeartbeatAt.IsZero() || mappedStatus == "" ||
		!validTransition(run.Status, mappedStatus) || !validRunnerOutcome(run.Scenario, snapshot.Status, snapshot.Outcome) {
		return ErrRunnerContract
	}
	if run.LastHeartbeatAt != nil && snapshot.HeartbeatAt.Before(*run.LastHeartbeatAt) {
		return ErrRunnerContract
	}
	if run.StartedAt != nil && (snapshot.StartedAt == nil || !snapshot.StartedAt.Equal(*run.StartedAt)) {
		return ErrRunnerContract
	}
	if run.TraceID != "" && (snapshot.TraceID != run.TraceID || snapshot.RootSpanID != run.RootSpanID) {
		return ErrRunnerContract
	}
	if snapshot.TraceID != "" && !telemetry.ValidTraceID(snapshot.TraceID) {
		return ErrRunnerContract
	}
	if snapshot.RootSpanID != "" && !telemetry.ValidSpanID(snapshot.RootSpanID) {
		return ErrRunnerContract
	}
	if (snapshot.TraceID == "") != (snapshot.RootSpanID == "") {
		return ErrRunnerContract
	}
	return nil
}

func mappedRunnerStatus(status string) model.DemoRunStatus {
	switch strings.TrimSpace(status) {
	case "queued":
		return model.DemoRunStarting
	case "starting":
		return model.DemoRunStarting
	case "running":
		return model.DemoRunRunning
	case "succeeded":
		return model.DemoRunSucceeded
	case "failed":
		return model.DemoRunFailed
	case "cancelled":
		return model.DemoRunCancelled
	default:
		return ""
	}
}

func validRunnerOutcome(scenario model.DemoScenario, status string, outcome model.DemoRunOutcome) bool {
	switch strings.TrimSpace(status) {
	case "queued", "starting", "running":
		return outcome == model.DemoOutcomeNone
	case "failed":
		return outcome == model.DemoOutcomeFailed
	case "cancelled":
		return outcome == model.DemoOutcomeCancelled
	case "succeeded":
		switch scenario {
		case model.DemoScenarioHappy:
			return outcome == model.DemoOutcomeNormal
		case model.DemoScenarioApproval:
			return outcome == model.DemoOutcomeApproved || outcome == model.DemoOutcomeDenied
		case model.DemoScenarioFailure:
			return outcome == model.DemoOutcomeDegraded
		}
	}
	return false
}

func projectApproval(approval model.Approval) *model.DemoApproval {
	return &model.DemoApproval{
		TicketID: approval.ID, UpstreamID: approval.UpstreamID, Source: approval.Source,
		FetchedAt: approval.FetchedAt, RawRef: approval.RawRef, SessionID: approval.SessionID,
		AgentID: approval.AgentID, AgentUpstreamID: approval.AgentUpstreamID,
		EventID: approval.EventID, EventType: approval.EventType, Tool: approval.Tool,
		Phase: approval.Phase, Action: approval.Action, Reason: approval.Reason,
		RiskScore: approval.RiskScore, MatchedRules: append([]string(nil), approval.MatchedRules...),
		Status: approval.Status, CreatedAt: approval.CreatedAt, CorrelationBasis: "session_id",
	}
}

func runnerEventType(previous, candidate model.DemoRun) string {
	if terminal(candidate.Status) && !terminal(previous.Status) {
		return "run.finished"
	}
	if candidate.Approval != nil && (previous.Approval == nil || previous.Approval.TicketID != candidate.Approval.TicketID) {
		return "run.approval_linked"
	}
	if candidate.TraceID != "" && previous.TraceID == "" {
		return "run.trace_linked"
	}
	if candidate.CurrentStep != previous.CurrentStep || candidate.CompletedSteps != previous.CompletedSteps {
		return "run.step"
	}
	if candidate.Status != previous.Status || candidate.StatusReasonCode != previous.StatusReasonCode {
		return "run.status"
	}
	return ""
}

func demoRunChanged(left, right model.DemoRun) bool {
	return left.Status != right.Status || left.Outcome != right.Outcome || left.StatusReasonCode != right.StatusReasonCode ||
		!sameTimePointer(left.StartedAt, right.StartedAt) || !sameTimePointer(left.CompletedAt, right.CompletedAt) ||
		!sameTimePointer(left.LastHeartbeatAt, right.LastHeartbeatAt) || left.TraceID != right.TraceID ||
		left.RootSpanID != right.RootSpanID || left.CurrentStep != right.CurrentStep ||
		left.CompletedSteps != right.CompletedSteps || left.TotalSteps != right.TotalSteps ||
		left.ErrorCode != right.ErrorCode || left.ErrorSummary != right.ErrorSummary || !sameDemoApproval(left.Approval, right.Approval)
}

func sameDemoApproval(left, right *model.DemoApproval) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.TicketID == right.TicketID && left.Status == right.Status
}

func sameTimePointer(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Equal(*right)
}

func timePointer(value time.Time) *time.Time { return &value }

func boundedRunnerErrorCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) > 64 {
		return "runner_execution_failed"
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return "runner_execution_failed"
		}
	}
	return value
}
