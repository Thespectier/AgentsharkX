package demo

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/stream"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
)

const (
	rootAgentID                  = "demo-incident-investigator"
	fixtureVersion               = "v1"
	maxDemoNoteBytes             = 500
	maxControlPersistenceRetries = 3
)

var (
	ErrDisabled     = errors.New("demo lab is disabled")
	ErrInvalid      = errors.New("invalid demo request")
	ErrNotReady     = errors.New("demo lab is not ready")
	ErrStateChanged = errors.New("demo run state changed")
)

type NotReadyError struct {
	FailedChecks []model.DemoStatusComponent
}

func (err *NotReadyError) Error() string { return ErrNotReady.Error() }

func (err *NotReadyError) Unwrap() error { return ErrNotReady }

type TraceReader interface {
	GetTraceSummary(context.Context, string) (telemetry.Summary, error)
}

type ApprovalService interface {
	PendingApprovalForSession(context.Context, string) (model.Approval, error)
	ResolveApproval(context.Context, string, string, model.ConfirmedActionRequest) (model.ProtectMutationEnvelope, error)
}

type GatewayLogReader interface {
	TrafficWindow(context.Context, int, model.TrendWindow) (model.AuditFeed, error)
}

type ComponentProbe struct {
	ID          string
	Label       string
	Required    bool
	Remediation string
	Check       func(context.Context) error
}

type Options struct {
	Enabled           bool
	DefaultDelayMS    int
	MaxConcurrency    int
	RunTimeout        time.Duration
	MonitorInterval   time.Duration
	RunnerLostAfter   time.Duration
	Now               func() time.Time
	Probes            []ComponentProbe
	GatewayLogs       GatewayLogReader
	GatewayConsoleURL string
}

type Service struct {
	controlMu sync.Mutex
	store     storage.DemoStore
	readiness storage.Readiness
	runner    Runner
	approvals ApprovalService
	traces    TraceReader
	hub       *stream.Hub
	options   Options
}

func New(store storage.DemoStore, readiness storage.Readiness, runner Runner, approvals ApprovalService, traces TraceReader, hub *stream.Hub, options Options) *Service {
	if options.DefaultDelayMS < 0 || options.DefaultDelayMS > 2000 {
		options.DefaultDelayMS = 700
	}
	if options.MaxConcurrency < 1 {
		options.MaxConcurrency = 1
	}
	if options.RunTimeout <= 0 {
		options.RunTimeout = 10 * time.Minute
	}
	if options.MonitorInterval <= 0 {
		options.MonitorInterval = 750 * time.Millisecond
	}
	if options.RunnerLostAfter <= 0 {
		options.RunnerLostAfter = 4 * options.MonitorInterval
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if hub == nil {
		hub = stream.NewHub()
	}
	return &Service{store: store, readiness: readiness, runner: runner, approvals: approvals, traces: traces, hub: hub, options: options}
}

func (service *Service) Enabled() bool { return service.options.Enabled }

func (service *Service) Status(ctx context.Context) model.DemoStatusEnvelope {
	now := service.options.Now().UTC()
	status := model.DemoStatus{
		Enabled: service.options.Enabled, Ready: service.options.Enabled,
		MaxConcurrency: service.options.MaxConcurrency, Components: []model.DemoStatusComponent{},
	}
	if service.store != nil {
		if active, err := service.store.GetActiveDemoRun(ctx); err == nil {
			status.ActiveRunID = &active.RunID
		}
	}
	probes := make([]ComponentProbe, 0, len(service.options.Probes)+2)
	if service.readiness != nil {
		probes = append(probes, ComponentProbe{
			ID: "database", Label: "Database", Required: true,
			Remediation: "Apply the current AgentsharkX migrations and restore PostgreSQL connectivity.",
			Check:       service.readiness.Ready,
		})
	}
	runnerProbe := ComponentProbe{
		ID: "demo-runner", Label: "Demo Runner", Required: true,
		Remediation: "Start the demo-runner service and verify its internal token configuration.",
	}
	if service.runner != nil {
		runnerProbe.Check = func(checkCtx context.Context) error {
			health, err := service.runner.Health(checkCtx)
			if err != nil {
				return err
			}
			if (health.ActiveRunID == nil) != (status.ActiveRunID == nil) ||
				(health.ActiveRunID != nil && status.ActiveRunID != nil && *health.ActiveRunID != *status.ActiveRunID) {
				return errors.New("Runner active Run does not match persisted control state")
			}
			return nil
		}
	}
	probes = append(probes, runnerProbe)
	probes = append(probes, service.options.Probes...)
	status.Components = make([]model.DemoStatusComponent, len(probes))
	var wait sync.WaitGroup
	for index, probe := range probes {
		component := model.DemoStatusComponent{
			ID: probe.ID, Label: probe.Label, Required: probe.Required,
			CheckedAt: now, Remediation: probe.Remediation, Status: model.HealthUnknown,
		}
		if !service.options.Enabled {
			component.Message = "Demo Lab is disabled"
		} else if probe.Check == nil {
			component.Message = "Readiness check is not configured"
		} else {
			status.Components[index] = component
			wait.Add(1)
			go func(index int, probe ComponentProbe) {
				defer wait.Done()
				probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()
				if err := probe.Check(probeCtx); err == nil {
					status.Components[index].Status = model.HealthHealthy
				} else {
					status.Components[index].Status = model.HealthDown
					status.Components[index].Message = boundedProbeMessage(err)
				}
			}(index, probe)
			continue
		}
		status.Components[index] = component
	}
	wait.Wait()
	for _, component := range status.Components {
		if component.Required && component.Status != model.HealthHealthy {
			status.Ready = false
		}
	}
	if !service.options.Enabled || service.store == nil || service.runner == nil {
		status.Ready = false
	}
	return model.DemoStatusEnvelope{Data: status, Meta: model.Meta{FetchedAt: now}}
}

func (service *Service) Scenarios() model.DemoScenariosEnvelope {
	now := service.options.Now().UTC()
	return model.DemoScenariosEnvelope{Data: []model.DemoScenarioDefinition{
		{ID: model.DemoScenarioHappy, Label: "Happy path", Description: "All deterministic capabilities complete normally.", ExpectedMetrics: expectedMetrics(model.DemoScenarioHappy)},
		{ID: model.DemoScenarioApproval, Label: "Approval", Description: "A simulated guarded action waits for an AgentGuard decision.", ExpectedMetrics: expectedMetrics(model.DemoScenarioApproval)},
		{ID: model.DemoScenarioFailure, Label: "Degraded", Description: "One deterministic MCP call fails and the run completes in degraded mode.", ExpectedMetrics: expectedMetrics(model.DemoScenarioFailure)},
	}, Meta: model.Meta{FetchedAt: now}}
}

func (service *Service) Create(ctx context.Context, request model.DemoCreateRunRequest, idempotencyKey, createdBy string) (model.DemoRunEnvelope, error) {
	if !service.options.Enabled {
		return model.DemoRunEnvelope{}, ErrDisabled
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if !validScenario(request.Scenario) || idempotencyKey == "" || len(idempotencyKey) > 256 {
		return model.DemoRunEnvelope{}, ErrInvalid
	}
	delay := service.options.DefaultDelayMS
	if request.DelayMS != nil {
		delay = *request.DelayMS
	}
	if delay < 0 || delay > 2000 {
		return model.DemoRunEnvelope{}, ErrInvalid
	}
	if existing, err := service.store.GetDemoRunByRequestID(ctx, idempotencyKey); err == nil {
		if existing.Scenario != request.Scenario || existing.DelayMS != delay {
			return model.DemoRunEnvelope{}, ErrStateChanged
		}
		return service.envelope(ctx, existing), nil
	} else if !errors.Is(err, storage.ErrDemoRunNotFound) {
		return model.DemoRunEnvelope{}, err
	}
	readiness := service.Status(ctx)
	if !readiness.Data.Ready {
		failedChecks := make([]model.DemoStatusComponent, 0, len(readiness.Data.Components))
		for _, component := range readiness.Data.Components {
			if component.Required && component.Status != model.HealthHealthy {
				failedChecks = append(failedChecks, component)
			}
		}
		return model.DemoRunEnvelope{}, &NotReadyError{FailedChecks: failedChecks}
	}
	runID, err := newUUID()
	if err != nil {
		return model.DemoRunEnvelope{}, err
	}
	now := service.options.Now().UTC()
	run := model.DemoRun{
		RunID: runID, Scenario: request.Scenario, Status: model.DemoRunQueued, Outcome: model.DemoOutcomeNone,
		RequestedAt: now, DelayMS: delay, TaskID: "demo-task-" + runID,
		SessionID: "demo-session-" + runID, RootAgentID: rootAgentID,
		TotalSteps: totalSteps(request.Scenario), FixtureVersion: fixtureVersion,
		RequestID: idempotencyKey, CreatedBy: strings.TrimSpace(createdBy),
	}
	created, sequence, err := service.store.CreateDemoRun(ctx, run, demoEvent(run, "run.status", now))
	if err != nil {
		return model.DemoRunEnvelope{}, err
	}
	service.notify(sequence)
	if sequence == 0 {
		if created.Scenario != request.Scenario || created.DelayMS != delay {
			return model.DemoRunEnvelope{}, ErrStateChanged
		}
		return service.envelope(ctx, created), nil
	}
	starting := created
	starting.Status = model.DemoRunStarting
	starting, _, err = service.persist(ctx, created, starting, "run.status")
	if err != nil {
		return model.DemoRunEnvelope{}, err
	}
	snapshot, err := service.runner.Start(ctx, RunnerStartRequest{
		RunID: starting.RunID, Scenario: starting.Scenario, DelayMS: starting.DelayMS,
		TaskID: starting.TaskID, SessionID: starting.SessionID, RequestID: starting.RequestID,
	})
	if err != nil {
		interrupted := starting
		failedAt := service.options.Now().UTC()
		interrupted.Status = model.DemoRunInterrupted
		interrupted.StatusReasonCode = runnerReasonCode(err)
		interrupted.Outcome = model.DemoOutcomeFailed
		interrupted.CompletedAt = &failedAt
		if _, _, persistErr := service.persist(ctx, starting, interrupted, "run.finished"); persistErr != nil {
			return model.DemoRunEnvelope{}, persistErr
		}
		return model.DemoRunEnvelope{}, err
	}
	updated, err := service.applyRunnerSnapshot(ctx, starting, snapshot)
	if err != nil {
		if errors.Is(err, ErrRunnerContract) {
			if _, persistErr := service.interruptForRunnerContract(ctx, starting); persistErr != nil {
				return model.DemoRunEnvelope{}, persistErr
			}
		}
		return model.DemoRunEnvelope{}, err
	}
	return service.envelope(ctx, updated), nil
}

func (service *Service) Get(ctx context.Context, runID string) (model.DemoRunEnvelope, error) {
	run, err := service.store.GetDemoRun(ctx, strings.TrimSpace(runID))
	if err != nil {
		return model.DemoRunEnvelope{}, err
	}
	return service.envelope(ctx, run), nil
}

func (service *Service) List(ctx context.Context, filter storage.DemoRunFilter) (model.DemoRunListEnvelope, error) {
	page, err := service.store.ListDemoRuns(ctx, filter)
	if err != nil {
		return model.DemoRunListEnvelope{}, err
	}
	items := make([]model.DemoRun, 0, len(page.Items))
	for _, run := range page.Items {
		items = append(items, service.enrich(ctx, run))
	}
	return model.DemoRunListEnvelope{
		Data: model.DemoRunPage{Items: items, NextCursor: page.NextCursor, Total: page.Total},
		Meta: model.Meta{FetchedAt: service.options.Now().UTC()},
	}, nil
}

func (service *Service) Cancel(ctx context.Context, runID string, request model.DemoCancelRunRequest) (model.DemoRunEnvelope, error) {
	if !service.options.Enabled {
		return model.DemoRunEnvelope{}, ErrDisabled
	}
	note := strings.TrimSpace(request.Note)
	if !request.Confirmed || note == "" || len([]byte(note)) > maxDemoNoteBytes {
		return model.DemoRunEnvelope{}, ErrInvalid
	}
	service.controlMu.Lock()
	defer service.controlMu.Unlock()

	run, err := service.store.GetDemoRun(ctx, strings.TrimSpace(runID))
	if err != nil {
		return model.DemoRunEnvelope{}, err
	}
	if terminal(run.Status) {
		return service.envelope(ctx, run), nil
	}
	deniedTicketID := ""
	if run.Status == model.DemoRunWaitingApproval && run.Approval != nil && strings.EqualFold(run.Approval.Status, "pending") {
		if service.approvals == nil {
			return model.DemoRunEnvelope{}, ErrStateChanged
		}
		if run.Approval.SessionID != run.SessionID {
			return model.DemoRunEnvelope{}, ErrStateChanged
		}
		_, err := service.approvals.ResolveApproval(ctx, run.Approval.TicketID, "deny", model.ConfirmedActionRequest{
			Confirmed: true, Note: "Demo Cancel: " + note,
		})
		if err != nil {
			return model.DemoRunEnvelope{}, ErrStateChanged
		}
		deniedTicketID = run.Approval.TicketID
	}
	snapshot, err := service.runner.Cancel(ctx, run.RunID)
	if errors.Is(err, ErrRunnerNotFound) {
		updated, persistErr := service.persistRunnerLostCancellation(ctx, run.RunID, deniedTicketID, run.SessionID)
		if persistErr != nil {
			return model.DemoRunEnvelope{}, persistErr
		}
		return service.envelope(ctx, updated), nil
	}
	if err != nil {
		if deniedTicketID != "" {
			if _, persistErr := service.persistDeniedCancellationApproval(ctx, run.RunID, deniedTicketID, run.SessionID); persistErr != nil {
				return model.DemoRunEnvelope{}, persistErr
			}
		}
		return model.DemoRunEnvelope{}, err
	}
	updated, err := service.persistRunnerCancellation(ctx, run.RunID, deniedTicketID, run.SessionID, snapshot)
	if err != nil {
		if errors.Is(err, ErrRunnerContract) {
			if _, persistErr := service.persistRunnerContractCancellation(ctx, run.RunID, deniedTicketID, run.SessionID); persistErr != nil {
				return model.DemoRunEnvelope{}, persistErr
			}
		}
		return model.DemoRunEnvelope{}, err
	}
	return service.envelope(ctx, updated), nil
}

func (service *Service) persistRunnerCancellation(ctx context.Context, runID, deniedTicketID, sessionID string, snapshot RunnerSnapshot) (model.DemoRun, error) {
	for range maxControlPersistenceRetries {
		_, candidate, err := service.loadCancellationState(ctx, runID, deniedTicketID, sessionID)
		if err != nil {
			return model.DemoRun{}, err
		}
		updated, err := service.applyRunnerSnapshot(ctx, candidate, snapshot)
		if errors.Is(err, ErrStateChanged) {
			continue
		}
		if err != nil {
			return model.DemoRun{}, err
		}
		if terminal(updated.Status) {
			if updated.Status != model.DemoRunCancelled || updated.Outcome != model.DemoOutcomeCancelled {
				return model.DemoRun{}, ErrStateChanged
			}
			return updated, nil
		}
		return service.persistCancelRequested(ctx, runID, deniedTicketID, sessionID)
	}
	return model.DemoRun{}, ErrStateChanged
}

func (service *Service) persistCancelRequested(ctx context.Context, runID, deniedTicketID, sessionID string) (model.DemoRun, error) {
	for range maxControlPersistenceRetries {
		previous, candidate, err := service.loadCancellationState(ctx, runID, deniedTicketID, sessionID)
		if err != nil {
			return model.DemoRun{}, err
		}
		candidate.StatusReasonCode = "cancel_requested"
		updated, _, err := service.persist(ctx, previous, candidate, "run.status")
		if errors.Is(err, ErrStateChanged) {
			continue
		}
		return updated, err
	}
	return model.DemoRun{}, ErrStateChanged
}

func (service *Service) persistRunnerLostCancellation(ctx context.Context, runID, deniedTicketID, sessionID string) (model.DemoRun, error) {
	for range maxControlPersistenceRetries {
		previous, candidate, err := service.loadCancellationState(ctx, runID, deniedTicketID, sessionID)
		if err != nil {
			return model.DemoRun{}, err
		}
		now := service.options.Now().UTC()
		candidate.Status = model.DemoRunInterrupted
		candidate.Outcome = model.DemoOutcomeCancelled
		candidate.StatusReasonCode = "runner_run_lost"
		candidate.CompletedAt = &now
		updated, _, err := service.persist(ctx, previous, candidate, "run.finished")
		if errors.Is(err, ErrStateChanged) {
			continue
		}
		return updated, err
	}
	return model.DemoRun{}, ErrStateChanged
}

func (service *Service) persistRunnerContractCancellation(ctx context.Context, runID, deniedTicketID, sessionID string) (model.DemoRun, error) {
	for range maxControlPersistenceRetries {
		previous, candidate, err := service.loadCancellationState(ctx, runID, deniedTicketID, sessionID)
		if err != nil {
			return model.DemoRun{}, err
		}
		now := service.options.Now().UTC()
		candidate.Status = model.DemoRunInterrupted
		candidate.Outcome = model.DemoOutcomeFailed
		candidate.StatusReasonCode = "runner_contract_mismatch"
		candidate.CompletedAt = &now
		updated, _, err := service.persist(ctx, previous, candidate, "run.finished")
		if errors.Is(err, ErrStateChanged) {
			continue
		}
		return updated, err
	}
	return model.DemoRun{}, ErrStateChanged
}

func (service *Service) persistDeniedCancellationApproval(ctx context.Context, runID, deniedTicketID, sessionID string) (model.DemoRun, error) {
	for range maxControlPersistenceRetries {
		previous, candidate, err := service.loadCancellationState(ctx, runID, deniedTicketID, sessionID)
		if err != nil {
			return model.DemoRun{}, err
		}
		updated, _, err := service.persist(ctx, previous, candidate, "run.status")
		if errors.Is(err, ErrStateChanged) {
			continue
		}
		return updated, err
	}
	return model.DemoRun{}, ErrStateChanged
}

func (service *Service) loadCancellationState(ctx context.Context, runID, deniedTicketID, sessionID string) (model.DemoRun, model.DemoRun, error) {
	previous, err := service.store.GetDemoRun(ctx, runID)
	if err != nil {
		return model.DemoRun{}, model.DemoRun{}, err
	}
	if terminal(previous.Status) {
		return model.DemoRun{}, model.DemoRun{}, ErrStateChanged
	}
	candidate := previous
	if deniedTicketID == "" {
		return previous, candidate, nil
	}
	if candidate.Approval == nil || candidate.Approval.TicketID != deniedTicketID ||
		candidate.Approval.SessionID != sessionID ||
		(!strings.EqualFold(candidate.Approval.Status, "pending") && !strings.EqualFold(candidate.Approval.Status, "denied")) {
		return model.DemoRun{}, model.DemoRun{}, ErrStateChanged
	}
	approval := *candidate.Approval
	approval.MatchedRules = append([]string(nil), candidate.Approval.MatchedRules...)
	approval.Status = "denied"
	candidate.Approval = &approval
	return previous, candidate, nil
}

func (service *Service) ReplayAfter(ctx context.Context, runID string, after int64, limit int) (storage.ReplayBatch, error) {
	return service.store.ReplayDemoAfter(ctx, runID, after, limit)
}

func (service *Service) StreamSnapshot(ctx context.Context, runID string) (storage.DemoStreamSnapshot, error) {
	snapshot, err := service.store.GetDemoStreamSnapshot(ctx, runID)
	if err != nil {
		return storage.DemoStreamSnapshot{}, err
	}
	snapshot.Run = service.enrich(ctx, snapshot.Run)
	return snapshot, nil
}

func (service *Service) envelope(ctx context.Context, run model.DemoRun) model.DemoRunEnvelope {
	return model.DemoRunEnvelope{Data: service.enrich(ctx, run), Meta: model.Meta{FetchedAt: service.options.Now().UTC()}}
}

func (service *Service) enrich(ctx context.Context, run model.DemoRun) model.DemoRun {
	run.ExpectedMetrics = expectedMetrics(run.Scenario)
	run.ObservedMetrics = nil
	run.Correlations = model.DemoCorrelations{RunID: run.RunID, TaskID: run.TaskID, SessionID: run.SessionID}
	run.Correlations.Trace = model.DemoCorrelationEvidence{Status: "pending", Basis: "runner_trace_id", Value: run.TraceID}
	run.Correlations.Approval = model.DemoCorrelationEvidence{Status: "pending", Basis: "session_id", Value: run.SessionID}
	run.Correlations.GatewayLogs = model.DemoCorrelationEvidence{
		Status: "pending", Basis: "agentgateway_exact_trace_id", Value: run.TraceID,
	}
	run.Links = model.DemoLinks{Audit: "/audit/security-events?sessionId=" + url.QueryEscape(run.SessionID)}
	if run.Approval != nil && run.Approval.SessionID == run.SessionID {
		run.Correlations.Approval.Status = "verified"
		run.Links.Approval = "/protect/approvals?ticketId=" + url.QueryEscape(run.Approval.TicketID)
	} else if terminal(run.Status) {
		run.Correlations.Approval.Status = "unavailable"
	}
	if run.TraceID == "" {
		run.Correlations.GatewayLogs.Status = "pending"
		if terminal(run.Status) {
			run.Correlations.Trace.Status = "unavailable"
			run.Correlations.GatewayLogs.Status = "unavailable"
			run.Correlations.GatewayLogs.Basis = "runner_trace_id_unavailable"
		}
		return run
	}
	service.enrichGatewayLogs(ctx, &run)
	if service.traces == nil {
		run.Correlations.Trace.Status = "unavailable"
		run.Correlations.Trace.Basis = "trace_store_unavailable"
		return run
	}
	summary, err := service.traces.GetTraceSummary(ctx, run.TraceID)
	if errors.Is(err, storage.ErrTraceNotFound) {
		return run
	}
	if err != nil || summary.TraceID != run.TraceID || summary.TaskID != run.TaskID || summary.SessionID != run.SessionID {
		run.Correlations.Trace.Status = "unavailable"
		run.Correlations.Trace.Basis = "identity_mismatch"
		return run
	}
	run.Correlations.Trace = model.DemoCorrelationEvidence{Status: "verified", Basis: "trace_id+task_id+session_id", Value: run.TraceID}
	run.Links.Trace = "/audit/traces/" + run.TraceID
	run.ObservedMetrics = &model.DemoMetrics{
		LLMCalls: summary.LLMCalls, MCPCalls: summary.MCPCalls, LocalToolCalls: summary.LocalToolCalls,
		A2ACalls: summary.A2ACalls, ErrorCount: summary.ErrorCount,
	}
	if run.Approval != nil {
		run.ObservedMetrics.HumanChecks = 1
	}
	return run
}

func (service *Service) enrichGatewayLogs(ctx context.Context, run *model.DemoRun) {
	if service.options.GatewayLogs == nil {
		run.Correlations.GatewayLogs.Status = "unavailable"
		run.Correlations.GatewayLogs.Basis = "agentgateway_log_source_unconfigured"
		return
	}
	now := service.options.Now().UTC()
	window := model.TrendWindow{
		From: run.RequestedAt.Add(-time.Minute).UTC(), To: now,
		BucketDuration: 5 * time.Minute,
	}
	feed, err := service.options.GatewayLogs.TrafficWindow(ctx, 500, window)
	if err != nil || feed.Status != "available" {
		run.Correlations.GatewayLogs.Status = "unavailable"
		run.Correlations.GatewayLogs.Basis = "agentgateway_log_source_unavailable"
		return
	}
	logIDs := map[string]struct{}{}
	for _, event := range feed.Events {
		if event.Source != model.SourceAgentGateway || event.RawRef.Source != model.SourceAgentGateway ||
			event.Correlation == nil || event.Correlation.TraceID != run.TraceID || strings.TrimSpace(event.RawRef.ID) == "" {
			continue
		}
		logIDs[event.RawRef.ID] = struct{}{}
	}
	if len(logIDs) == 0 {
		run.Correlations.GatewayLogs.Status = "unavailable"
		run.Correlations.GatewayLogs.Basis = "agentgateway_exact_trace_evidence_unavailable"
		return
	}
	expectedLogs := expectedMetrics(run.Scenario).LLMCalls
	if len(logIDs) != expectedLogs {
		run.Correlations.GatewayLogs.Status = "unavailable"
		if len(logIDs) < expectedLogs {
			run.Correlations.GatewayLogs.Basis = "agentgateway_exact_trace_evidence_incomplete"
		} else {
			run.Correlations.GatewayLogs.Basis = "agentgateway_exact_trace_evidence_count_mismatch"
		}
		return
	}
	logID := ""
	for candidate := range logIDs {
		if logID == "" || candidate < logID {
			logID = candidate
		}
	}
	run.Correlations.GatewayLogs.Status = "verified"
	run.Correlations.GatewayLogs.Basis = "complete_identical_agentgateway_trace_id_set"
	run.Links.GatewayLogs = gatewayLogURL(service.options.GatewayConsoleURL, logID)
}

func gatewayLogURL(adminURL, logID string) string {
	parsed, err := url.Parse(strings.TrimSpace(adminURL))
	if err != nil || parsed.Host == "" {
		return ""
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/ui/llm/logs"
	query := parsed.Query()
	query.Set("log", logID)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (service *Service) persist(ctx context.Context, previous, candidate model.DemoRun, eventType string) (model.DemoRun, int64, error) {
	if !validTransition(previous.Status, candidate.Status) {
		return model.DemoRun{}, 0, ErrStateChanged
	}
	now := service.options.Now().UTC()
	event := demoEvent(candidate, eventType, now)
	updated, sequence, err := service.store.UpdateDemoRun(ctx, storage.DemoMutation{
		Run: candidate, ExpectedVersion: previous.RunVersion, Event: event,
	})
	if errors.Is(err, storage.ErrDemoRunConflict) {
		return model.DemoRun{}, 0, ErrStateChanged
	}
	if err != nil {
		return model.DemoRun{}, 0, err
	}
	service.notify(sequence)
	return updated, sequence, nil
}

func (service *Service) interruptForRunnerContract(ctx context.Context, run model.DemoRun) (model.DemoRun, error) {
	interrupted := run
	now := service.options.Now().UTC()
	interrupted.Status = model.DemoRunInterrupted
	interrupted.Outcome = model.DemoOutcomeFailed
	interrupted.StatusReasonCode = "runner_contract_mismatch"
	interrupted.CompletedAt = &now
	updated, _, err := service.persist(ctx, run, interrupted, "run.finished")
	return updated, err
}

func (service *Service) notify(sequence int64) {
	if sequence > 0 && service.hub != nil {
		service.hub.Notify()
	}
}

func demoEvent(run model.DemoRun, eventType string, now time.Time) model.DemoRunEvent {
	event := model.DemoRunEvent{
		RunID: run.RunID, Type: eventType, Status: run.Status, Outcome: run.Outcome,
		RunVersion: run.RunVersion, StepID: run.CurrentStep, TraceID: run.TraceID,
		RootSpanID: run.RootSpanID, CompletedSteps: run.CompletedSteps,
		TotalSteps: run.TotalSteps, OccurredAt: now,
	}
	if run.Approval != nil {
		approval := *run.Approval
		approval.MatchedRules = append([]string(nil), run.Approval.MatchedRules...)
		event.Approval = &approval
	}
	return event
}

func expectedMetrics(scenario model.DemoScenario) model.DemoMetrics {
	metrics := model.DemoMetrics{LLMCalls: 3, MCPCalls: 2, LocalToolCalls: 1, A2ACalls: 1}
	if scenario == model.DemoScenarioApproval {
		metrics.LocalToolCalls = 2
		metrics.HumanChecks = 1
	}
	if scenario == model.DemoScenarioFailure {
		metrics.ErrorCount = 1
	}
	return metrics
}

func totalSteps(scenario model.DemoScenario) int {
	if scenario == model.DemoScenarioApproval {
		return 10
	}
	return 9
}

func validScenario(scenario model.DemoScenario) bool {
	return scenario == model.DemoScenarioHappy || scenario == model.DemoScenarioApproval || scenario == model.DemoScenarioFailure
}

func terminal(status model.DemoRunStatus) bool {
	return status == model.DemoRunSucceeded || status == model.DemoRunFailed || status == model.DemoRunCancelled || status == model.DemoRunInterrupted || status == model.DemoRunExpired
}

func validTransition(from, to model.DemoRunStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case model.DemoRunQueued:
		return to == model.DemoRunStarting || to == model.DemoRunCancelled || to == model.DemoRunInterrupted || to == model.DemoRunFailed
	case model.DemoRunStarting:
		// A zero-delay run can reach Human Check between monitor polls, so the
		// persisted view may legitimately collapse running into this transition.
		return to == model.DemoRunRunning || to == model.DemoRunWaitingApproval || to == model.DemoRunSucceeded || to == model.DemoRunFailed || to == model.DemoRunCancelled || to == model.DemoRunInterrupted
	case model.DemoRunRunning:
		return to == model.DemoRunWaitingApproval || to == model.DemoRunSucceeded || to == model.DemoRunFailed || to == model.DemoRunCancelled || to == model.DemoRunInterrupted
	case model.DemoRunWaitingApproval:
		return to == model.DemoRunRunning || to == model.DemoRunSucceeded || to == model.DemoRunFailed || to == model.DemoRunCancelled || to == model.DemoRunInterrupted || to == model.DemoRunExpired
	default:
		return false
	}
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func boundedProbeMessage(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 200 {
		message = message[:200]
	}
	return message
}

func runnerReasonCode(err error) string {
	switch {
	case errors.Is(err, ErrRunnerBusy):
		return "runner_busy"
	case errors.Is(err, ErrRunnerConflict):
		return "runner_state_conflict"
	case errors.Is(err, ErrRunnerContract):
		return "runner_contract_mismatch"
	default:
		return "runner_unavailable"
	}
}
