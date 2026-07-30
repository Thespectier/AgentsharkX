package demo

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage/memory"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/stream"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
)

type runnerStub struct {
	mu           sync.Mutex
	now          func() time.Time
	request      RunnerStartRequest
	startCalls   int
	cancelCalls  int
	healthError  error
	startError   error
	getError     error
	invalidStart bool
	startQueued  bool
	snapshot     RunnerSnapshot
	order        *[]string
}

func (runner *runnerStub) Health(context.Context) (RunnerHealth, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.healthError != nil {
		return RunnerHealth{}, runner.healthError
	}
	var active *string
	if runner.request.RunID != "" && runner.snapshot.Status != "succeeded" && runner.snapshot.Status != "failed" && runner.snapshot.Status != "cancelled" {
		value := runner.request.RunID
		active = &value
	}
	return RunnerHealth{Status: "healthy", Service: "agentshark-demo-runner", MaxConcurrency: 1, ActiveRunID: active}, nil
}

func (runner *runnerStub) Start(_ context.Context, request RunnerStartRequest) (RunnerSnapshot, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.request = request
	runner.startCalls++
	if runner.startError != nil {
		return RunnerSnapshot{}, runner.startError
	}
	startedAt := runner.now()
	runner.snapshot = RunnerSnapshot{
		RunID: request.RunID, Scenario: request.Scenario, Status: "running", Outcome: model.DemoOutcomeNone,
		DelayMS: request.DelayMS, TaskID: request.TaskID, SessionID: request.SessionID,
		RequestID: request.RequestID, CurrentStep: "bootstrap", TotalSteps: totalSteps(request.Scenario),
		StartedAt: &startedAt, HeartbeatAt: startedAt,
	}
	if runner.startQueued {
		runner.snapshot.Status = "queued"
		runner.snapshot.CurrentStep = ""
		runner.snapshot.StartedAt = nil
	}
	if runner.invalidStart {
		runner.snapshot.RunID = "different-run"
	}
	return runner.snapshot, nil
}

func (runner *runnerStub) Get(context.Context, string) (RunnerSnapshot, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.getError != nil {
		return RunnerSnapshot{}, runner.getError
	}
	return runner.snapshot, nil
}

func (runner *runnerStub) Cancel(context.Context, string) (RunnerSnapshot, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	now := runner.now()
	runner.snapshot.Status = "cancelled"
	runner.snapshot.Outcome = model.DemoOutcomeCancelled
	runner.snapshot.CompletedAt = &now
	runner.snapshot.HeartbeatAt = now
	runner.cancelCalls++
	if runner.order != nil {
		*runner.order = append(*runner.order, "cancel")
	}
	return runner.snapshot, nil
}

type approvalStub struct {
	approval        model.Approval
	err             error
	resolveErr      error
	resolveCalls    int
	resolvedTicket  string
	resolvedAction  string
	resolvedRequest model.ConfirmedActionRequest
	order           *[]string
}

func (stub *approvalStub) PendingApprovalForSession(_ context.Context, sessionID string) (model.Approval, error) {
	if stub.err != nil {
		return model.Approval{}, stub.err
	}
	if stub.approval.SessionID != sessionID {
		return model.Approval{}, errors.New("approval stub received a non-identical session ID")
	}
	return stub.approval, nil
}

func (stub *approvalStub) ResolveApproval(_ context.Context, ticketID, action string, request model.ConfirmedActionRequest) (model.ProtectMutationEnvelope, error) {
	stub.resolveCalls++
	stub.resolvedTicket = ticketID
	stub.resolvedAction = action
	stub.resolvedRequest = request
	if stub.order != nil {
		*stub.order = append(*stub.order, "deny")
	}
	return model.ProtectMutationEnvelope{}, stub.resolveErr
}

type updateInterceptStore struct {
	storage.DemoStore
	mu                sync.Mutex
	beforeFirstUpdate func(context.Context, storage.DemoMutation) error
	injected          bool
}

func (store *updateInterceptStore) UpdateDemoRun(ctx context.Context, mutation storage.DemoMutation) (model.DemoRun, int64, error) {
	store.mu.Lock()
	hook := store.beforeFirstUpdate
	if !store.injected {
		store.injected = true
	} else {
		hook = nil
	}
	store.mu.Unlock()
	if hook != nil {
		if err := hook(ctx, mutation); err != nil {
			return model.DemoRun{}, 0, err
		}
	}
	return store.DemoStore.UpdateDemoRun(ctx, mutation)
}

func (store *updateInterceptStore) didInject() bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.injected
}

type traceStub struct {
	summary telemetry.Summary
	err     error
}

type gatewayLogStub struct {
	feed model.AuditFeed
	err  error
}

func (stub *gatewayLogStub) TrafficWindow(context.Context, int, model.TrendWindow) (model.AuditFeed, error) {
	return stub.feed, stub.err
}

func (stub *traceStub) GetTraceSummary(context.Context, string) (telemetry.Summary, error) {
	return stub.summary, stub.err
}

func newTestService(now *time.Time, runner *runnerStub, approvals ApprovalService, traces TraceReader) (*Service, *memory.Store) {
	clock := func() time.Time { return now.UTC() }
	store := memory.New(memory.Options{Now: clock, OutboxRetention: time.Hour})
	service := New(store, store, runner, approvals, traces, stream.NewHub(), Options{
		Enabled: true, DefaultDelayMS: 700, MaxConcurrency: 1, RunTimeout: time.Hour,
		MonitorInterval: 500 * time.Millisecond, RunnerLostAfter: 2 * time.Second, Now: clock,
	})
	return service, store
}

func prepareWaitingApprovalRun(t *testing.T, now *time.Time, service *Service, runner *runnerStub, approvals *approvalStub, requestID, ticketID string) model.DemoRun {
	t.Helper()
	created, err := service.Create(t.Context(), model.DemoCreateRunRequest{Scenario: model.DemoScenarioApproval}, requestID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	runner.mu.Lock()
	runner.snapshot.CurrentStep = "guarded_action"
	runner.snapshot.CompletedSteps = 7
	runner.snapshot.HeartbeatAt = now.UTC()
	runner.mu.Unlock()
	approvals.err = nil
	approvals.approval = model.Approval{
		ProtectResourceBase: model.ProtectResourceBase{
			ID: ticketID, UpstreamID: "upstream-" + ticketID, Source: model.SourceAgentGuard,
			FetchedAt: now.UTC(), RawRef: model.RawRef{Source: model.SourceAgentGuard, ID: "/v1/backend/approvals/0"},
		},
		SessionID: created.Data.SessionID, AgentID: rootAgentID, EventID: "event-" + requestID,
		EventType: "tool_invoke", Tool: "simulated_action", Phase: "tool_before",
		Action: "human_check", RiskScore: 0.8, MatchedRules: []string{"demo_tripwire"},
		Status: "pending", CreatedAt: now.UTC(),
	}
	if err := service.MonitorOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	run, err := service.store.GetDemoRun(t.Context(), created.Data.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != model.DemoRunWaitingApproval || run.Approval == nil || run.Approval.TicketID != ticketID {
		t.Fatalf("waiting approval run = %#v", run)
	}
	return run
}

func TestCreateIsRequestIDIdempotentAndEnforcesSingleActiveRun(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	runner := &runnerStub{now: func() time.Time { return now }}
	service, _ := newTestService(&now, runner, nil, nil)
	delay := 0
	request := model.DemoCreateRunRequest{Scenario: model.DemoScenarioHappy, DelayMS: &delay}
	first, err := service.Create(t.Context(), request, "request-one", "admin")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(t.Context(), request, "request-one", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if first.Data.RunID != second.Data.RunID || runner.startCalls != 1 {
		t.Fatalf("idempotent create started a duplicate: first=%s second=%s calls=%d", first.Data.RunID, second.Data.RunID, runner.startCalls)
	}
	differentDelay := 1
	if _, err := service.Create(t.Context(), model.DemoCreateRunRequest{
		Scenario: model.DemoScenarioHappy, DelayMS: &differentDelay,
	}, "request-one", "admin"); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("same request ID with different delay error = %v", err)
	}
	if _, err := service.Create(t.Context(), model.DemoCreateRunRequest{
		Scenario: model.DemoScenarioFailure, DelayMS: &delay,
	}, "request-one", "admin"); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("same request ID with different scenario error = %v", err)
	}
	if _, err := service.Create(t.Context(), request, "request-two", "admin"); !errors.Is(err, storage.ErrDemoRunBusy) {
		t.Fatalf("second active create error = %v", err)
	}
}

func TestCreateContractMismatchImmediatelyInterruptsPersistedRun(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	runner := &runnerStub{now: func() time.Time { return now }, invalidStart: true}
	service, store := newTestService(&now, runner, nil, nil)

	_, err := service.Create(t.Context(), model.DemoCreateRunRequest{Scenario: model.DemoScenarioHappy}, "bad-contract", "admin")
	if !errors.Is(err, ErrRunnerContract) {
		t.Fatalf("create error = %v", err)
	}
	run, err := store.GetDemoRunByRequestID(t.Context(), "bad-contract")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != model.DemoRunInterrupted || run.Outcome != model.DemoOutcomeFailed ||
		run.StatusReasonCode != "runner_contract_mismatch" || run.CompletedAt == nil {
		t.Fatalf("persisted run = %#v", run)
	}
	if _, err := store.GetActiveDemoRun(t.Context()); !errors.Is(err, storage.ErrDemoRunNotFound) {
		t.Fatalf("active run remained after contract mismatch: %v", err)
	}
}

func TestCreateDisabledNotReadyAndRunnerRaceFailures(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	runner := &runnerStub{now: func() time.Time { return now }}
	service, store := newTestService(&now, runner, nil, nil)
	service.options.Enabled = false
	if _, err := service.Create(t.Context(), model.DemoCreateRunRequest{Scenario: model.DemoScenarioHappy}, "disabled", "admin"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled create error = %v", err)
	}

	service.options.Enabled = true
	runner.healthError = ErrRunnerUnavailable
	service.options.Probes = append(service.options.Probes, ComponentProbe{
		ID: "optional-observer", Label: "Optional observer", Required: false,
		Check: func(context.Context) error { return errors.New("optional observer unavailable") },
	})
	_, err := service.Create(t.Context(), model.DemoCreateRunRequest{Scenario: model.DemoScenarioHappy}, "not-ready", "admin")
	var notReady *NotReadyError
	if !errors.Is(err, ErrNotReady) || !errors.As(err, &notReady) {
		t.Fatalf("not-ready create error = %v", err)
	}
	if len(notReady.FailedChecks) != 1 || notReady.FailedChecks[0].ID != "demo-runner" || !notReady.FailedChecks[0].Required {
		t.Fatalf("failed readiness checks = %#v", notReady.FailedChecks)
	}
	if _, err := store.GetDemoRunByRequestID(t.Context(), "not-ready"); !errors.Is(err, storage.ErrDemoRunNotFound) {
		t.Fatalf("not-ready request was persisted: %v", err)
	}

	runner.healthError = nil
	runner.startError = ErrRunnerBusy
	if _, err := service.Create(t.Context(), model.DemoCreateRunRequest{Scenario: model.DemoScenarioHappy}, "runner-race", "admin"); !errors.Is(err, ErrRunnerBusy) {
		t.Fatalf("runner race create error = %v", err)
	}
	run, err := store.GetDemoRunByRequestID(t.Context(), "runner-race")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != model.DemoRunInterrupted || run.StatusReasonCode != "runner_busy" {
		t.Fatalf("runner race persisted run = %#v", run)
	}
}

func TestStatusUsesFrozenDemoRunnerComponentID(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	runner := &runnerStub{now: func() time.Time { return now }}
	service, _ := newTestService(&now, runner, nil, nil)
	status := service.Status(t.Context())
	for _, component := range status.Data.Components {
		if component.ID == "demo-runner" {
			return
		}
	}
	t.Fatalf("demo-runner component missing: %#v", status.Data.Components)
}

func TestDemoRunTransitions(t *testing.T) {
	t.Parallel()
	statuses := []model.DemoRunStatus{
		model.DemoRunQueued, model.DemoRunStarting, model.DemoRunRunning,
		model.DemoRunWaitingApproval, model.DemoRunSucceeded, model.DemoRunFailed,
		model.DemoRunCancelled, model.DemoRunInterrupted, model.DemoRunExpired,
	}
	allowed := map[model.DemoRunStatus]map[model.DemoRunStatus]bool{
		model.DemoRunQueued: {
			model.DemoRunStarting: true, model.DemoRunCancelled: true,
			model.DemoRunInterrupted: true, model.DemoRunFailed: true,
		},
		model.DemoRunStarting: {
			model.DemoRunRunning: true, model.DemoRunWaitingApproval: true,
			model.DemoRunSucceeded: true,
			model.DemoRunFailed:    true, model.DemoRunCancelled: true,
			model.DemoRunInterrupted: true,
		},
		model.DemoRunRunning: {
			model.DemoRunWaitingApproval: true, model.DemoRunSucceeded: true,
			model.DemoRunFailed: true, model.DemoRunCancelled: true,
			model.DemoRunInterrupted: true,
		},
		model.DemoRunWaitingApproval: {
			model.DemoRunRunning: true, model.DemoRunSucceeded: true,
			model.DemoRunFailed: true, model.DemoRunCancelled: true,
			model.DemoRunInterrupted: true, model.DemoRunExpired: true,
		},
	}
	for _, from := range statuses {
		for _, to := range statuses {
			want := from == to || allowed[from][to]
			if got := validTransition(from, to); got != want {
				t.Errorf("validTransition(%q, %q) = %t, want %t", from, to, got, want)
			}
		}
	}
}

func TestRunnerSnapshotRejectsStateRegressionAndIdentityDrift(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	traceID := "11111111111111111111111111111111"
	spanID := "2222222222222222"
	run := model.DemoRun{
		RunID: "11111111-1111-4111-8111-111111111111", Scenario: model.DemoScenarioHappy,
		Status: model.DemoRunRunning, Outcome: model.DemoOutcomeNone, RequestedAt: now,
		StartedAt: &now, LastHeartbeatAt: &now, DelayMS: 0, TaskID: "task", SessionID: "session",
		TraceID: traceID, RootSpanID: spanID, TotalSteps: 9, CompletedSteps: 3, RequestID: "request",
	}
	valid := RunnerSnapshot{
		RunID: run.RunID, Scenario: run.Scenario, Status: "running", Outcome: model.DemoOutcomeNone,
		DelayMS: run.DelayMS, TaskID: run.TaskID, SessionID: run.SessionID, RequestID: run.RequestID,
		TraceID: traceID, RootSpanID: spanID, CurrentStep: "analyze_evidence",
		CompletedSteps: 3, TotalSteps: 9, StartedAt: &now, HeartbeatAt: now,
	}
	if err := validateRunnerSnapshot(run, valid); err != nil {
		t.Fatalf("valid snapshot = %v", err)
	}
	tests := map[string]func(*RunnerSnapshot){
		"status regression": func(snapshot *RunnerSnapshot) { snapshot.Status = "queued" },
		"step regression":   func(snapshot *RunnerSnapshot) { snapshot.CompletedSteps = 2 },
		"trace drift":       func(snapshot *RunnerSnapshot) { snapshot.TraceID = "33333333333333333333333333333333" },
		"heartbeat rewind":  func(snapshot *RunnerSnapshot) { snapshot.HeartbeatAt = now.Add(-time.Second) },
		"active outcome":    func(snapshot *RunnerSnapshot) { snapshot.Outcome = model.DemoOutcomeNormal },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			snapshot := valid
			mutate(&snapshot)
			if err := validateRunnerSnapshot(run, snapshot); !errors.Is(err, ErrRunnerContract) {
				t.Fatalf("validation error = %v", err)
			}
		})
	}
}

func TestMonitorLinksApprovalOnlyThroughIdenticalSessionID(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	fetchedAt := now.Add(time.Second)
	runner := &runnerStub{now: func() time.Time { return now }}
	approvals := &approvalStub{err: errors.New("not configured")}
	service, _ := newTestService(&now, runner, approvals, nil)
	created, err := service.Create(t.Context(), model.DemoCreateRunRequest{Scenario: model.DemoScenarioApproval}, "approval-request", "admin")
	if err != nil {
		t.Fatal(err)
	}
	runner.snapshot.CurrentStep = "guarded_action"
	runner.snapshot.CompletedSteps = 7
	approvals.err = nil
	approvals.approval = model.Approval{
		ProtectResourceBase: model.ProtectResourceBase{
			ID: "ticket-demo", UpstreamID: "ticket-upstream-demo", Source: model.SourceAgentGuard,
			FetchedAt: fetchedAt, RawRef: model.RawRef{Source: model.SourceAgentGuard, ID: "/v1/backend/approvals/0"},
		},
		SessionID: created.Data.SessionID, AgentID: rootAgentID, EventID: "event-demo",
		EventType: "tool_invoke", Tool: "simulated_action", Phase: "tool_before",
		Action: "human_check", RiskScore: 0.8, MatchedRules: []string{"demo_tripwire"},
		Status: "pending", CreatedAt: now,
	}
	if err := service.MonitorOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(t.Context(), created.Data.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Data.Status != model.DemoRunWaitingApproval || current.Data.Approval == nil ||
		current.Data.Approval.SessionID != created.Data.SessionID || current.Data.Correlations.Approval.Status != "verified" ||
		current.Data.Correlations.Approval.Basis != "session_id" {
		t.Fatalf("approval was not exactly session-linked: %#v", current.Data)
	}
	if current.Data.Approval.TicketID != "ticket-demo" || current.Data.Approval.UpstreamID != "ticket-upstream-demo" ||
		!current.Data.Approval.FetchedAt.Equal(fetchedAt) ||
		current.Data.Approval.RawRef != (model.RawRef{Source: model.SourceAgentGuard, ID: "/v1/backend/approvals/0"}) {
		t.Fatalf("approval source identity was not preserved: %#v", current.Data.Approval)
	}
	if current.Data.Links.Audit != "/audit/security-events?sessionId="+created.Data.SessionID ||
		current.Data.Links.Approval != "/protect/approvals?ticketId=ticket-demo" {
		t.Fatalf("demo evidence links are not valid deep links: %#v", current.Data.Links)
	}
}

func TestMonitorCanFirstObserveFastRunAtWaitingApproval(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	runner := &runnerStub{now: func() time.Time { return now }, startQueued: true}
	approvals := &approvalStub{err: errors.New("not configured")}
	service, _ := newTestService(&now, runner, approvals, nil)
	created, err := service.Create(t.Context(), model.DemoCreateRunRequest{
		Scenario: model.DemoScenarioApproval,
	}, "fast-approval-request", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if created.Data.Status != model.DemoRunStarting {
		t.Fatalf("created status = %q", created.Data.Status)
	}

	now = now.Add(time.Millisecond)
	runner.snapshot.Status = "running"
	runner.snapshot.StartedAt = timePointer(now)
	runner.snapshot.HeartbeatAt = now
	runner.snapshot.TraceID = "11111111111111111111111111111111"
	runner.snapshot.RootSpanID = "2222222222222222"
	runner.snapshot.CurrentStep = "guarded_action"
	runner.snapshot.CompletedSteps = 7
	approvals.err = nil
	approvals.approval = model.Approval{
		ProtectResourceBase: model.ProtectResourceBase{
			ID: "ticket-fast", UpstreamID: "ticket-upstream-fast", Source: model.SourceAgentGuard,
			FetchedAt: now, RawRef: model.RawRef{Source: model.SourceAgentGuard, ID: "/v1/backend/approvals/0"},
		},
		SessionID: created.Data.SessionID, AgentID: rootAgentID, EventID: "event-fast",
		EventType: "tool_invoke", Tool: "send_http", Phase: "tool_before",
		Action: "human_check", RiskScore: 0.8, MatchedRules: []string{"demo_tripwire"},
		Status: "pending", CreatedAt: now,
	}

	if err := service.MonitorOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(t.Context(), created.Data.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Data.Status != model.DemoRunWaitingApproval || current.Data.Approval == nil ||
		current.Data.Approval.TicketID != "ticket-fast" || current.Data.TraceID != runner.snapshot.TraceID {
		t.Fatalf("fast approval snapshot was not persisted: %#v", current.Data)
	}
}

func TestCancelWaitingApprovalDeniesBeforeRunnerCancellation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	order := []string{}
	runner := &runnerStub{now: func() time.Time { return now }, order: &order}
	approvals := &approvalStub{err: errors.New("not configured"), order: &order}
	service, store := newTestService(&now, runner, approvals, nil)
	run := prepareWaitingApprovalRun(t, &now, service, runner, approvals, "cancel-waiting", "ticket-cancel")

	result, err := service.Cancel(t.Context(), run.RunID, model.DemoCancelRunRequest{Confirmed: true, Note: "operator stop"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Data.Status != model.DemoRunCancelled || result.Data.Outcome != model.DemoOutcomeCancelled ||
		result.Data.Approval == nil || result.Data.Approval.Status != "denied" {
		t.Fatalf("cancel result = %#v", result.Data)
	}
	if approvals.resolveCalls != 1 || approvals.resolvedTicket != "ticket-cancel" || approvals.resolvedAction != "deny" ||
		!approvals.resolvedRequest.Confirmed || approvals.resolvedRequest.Note != "Demo Cancel: operator stop" {
		t.Fatalf("approval resolution = calls:%d ticket:%q action:%q request:%#v", approvals.resolveCalls, approvals.resolvedTicket, approvals.resolvedAction, approvals.resolvedRequest)
	}
	if runner.cancelCalls != 1 || len(order) != 2 || order[0] != "deny" || order[1] != "cancel" {
		t.Fatalf("cancel ordering = calls:%d order:%v", runner.cancelCalls, order)
	}
	persisted, err := store.GetDemoRun(t.Context(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.DemoRunCancelled || persisted.Approval == nil || persisted.Approval.Status != "denied" {
		t.Fatalf("persisted cancel = %#v", persisted)
	}
}

func TestCancelContinuesAfterApprovalDenialWhenVersionAdvances(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	runner := &runnerStub{now: func() time.Time { return now }}
	approvals := &approvalStub{err: errors.New("not configured")}
	service, store := newTestService(&now, runner, approvals, nil)
	run := prepareWaitingApprovalRun(t, &now, service, runner, approvals, "cancel-conflict", "ticket-conflict")
	interceptor := &updateInterceptStore{DemoStore: store}
	interceptor.beforeFirstUpdate = func(ctx context.Context, mutation storage.DemoMutation) error {
		current, err := store.GetDemoRun(ctx, mutation.Run.RunID)
		if err != nil {
			return err
		}
		_, _, err = store.UpdateDemoRun(ctx, storage.DemoMutation{Run: current, ExpectedVersion: current.RunVersion})
		return err
	}
	service.store = interceptor

	result, err := service.Cancel(t.Context(), run.RunID, model.DemoCancelRunRequest{Confirmed: true, Note: "conflict retry"})
	if err != nil {
		t.Fatal(err)
	}
	if !interceptor.didInject() || approvals.resolveCalls != 1 || runner.cancelCalls != 1 ||
		result.Data.Status != model.DemoRunCancelled || result.Data.Approval == nil || result.Data.Approval.Status != "denied" {
		t.Fatalf("retry result: injected=%t approvals=%d cancels=%d run=%#v", interceptor.didInject(), approvals.resolveCalls, runner.cancelCalls, result.Data)
	}
}

func TestCancelReturnsStateChangedWithoutOverwritingConcurrentTerminal(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	runner := &runnerStub{now: func() time.Time { return now }}
	approvals := &approvalStub{err: errors.New("not configured")}
	service, store := newTestService(&now, runner, approvals, nil)
	run := prepareWaitingApprovalRun(t, &now, service, runner, approvals, "cancel-terminal", "ticket-terminal")
	interceptor := &updateInterceptStore{DemoStore: store}
	interceptor.beforeFirstUpdate = func(ctx context.Context, mutation storage.DemoMutation) error {
		current, err := store.GetDemoRun(ctx, mutation.Run.RunID)
		if err != nil {
			return err
		}
		completedAt := now.UTC()
		current.Status = model.DemoRunSucceeded
		current.Outcome = model.DemoOutcomeDenied
		current.CompletedAt = &completedAt
		approval := *current.Approval
		approval.Status = "denied"
		current.Approval = &approval
		_, _, err = store.UpdateDemoRun(ctx, storage.DemoMutation{Run: current, ExpectedVersion: current.RunVersion})
		return err
	}
	service.store = interceptor

	_, err := service.Cancel(t.Context(), run.RunID, model.DemoCancelRunRequest{Confirmed: true, Note: "terminal wins"})
	if !errors.Is(err, ErrStateChanged) {
		t.Fatalf("cancel error = %v", err)
	}
	persisted, getErr := store.GetDemoRun(t.Context(), run.RunID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if approvals.resolveCalls != 1 || runner.cancelCalls != 1 || persisted.Status != model.DemoRunSucceeded ||
		persisted.Outcome != model.DemoOutcomeDenied || persisted.Approval == nil || persisted.Approval.Status != "denied" {
		t.Fatalf("terminal winner was overwritten: approvals=%d cancels=%d run=%#v", approvals.resolveCalls, runner.cancelCalls, persisted)
	}
}

func TestCancelReturnsStateChangedWhenApprovalResolutionAlreadyCompleted(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	runner := &runnerStub{now: func() time.Time { return now }}
	approvals := &approvalStub{err: errors.New("not configured")}
	service, store := newTestService(&now, runner, approvals, nil)
	run := prepareWaitingApprovalRun(t, &now, service, runner, approvals, "cancel-approval-race", "ticket-approval-race")
	approvals.resolveErr = errors.New("approval is no longer pending")

	_, err := service.Cancel(t.Context(), run.RunID, model.DemoCancelRunRequest{Confirmed: true, Note: "approval wins"})
	if !errors.Is(err, ErrStateChanged) {
		t.Fatalf("cancel error = %v", err)
	}
	persisted, getErr := store.GetDemoRun(t.Context(), run.RunID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if approvals.resolveCalls != 1 || runner.cancelCalls != 0 || persisted.Status != model.DemoRunWaitingApproval {
		t.Fatalf("approval race result: approvals=%d cancels=%d run=%#v", approvals.resolveCalls, runner.cancelCalls, persisted)
	}
}

func TestMonitorMarksLostRunnerInterrupted(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	runner := &runnerStub{now: func() time.Time { return now }}
	service, _ := newTestService(&now, runner, nil, nil)
	created, err := service.Create(t.Context(), model.DemoCreateRunRequest{Scenario: model.DemoScenarioFailure}, "lost-request", "admin")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Second)
	runner.getError = ErrRunnerNotFound
	if err := service.MonitorOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(t.Context(), created.Data.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Data.Status != model.DemoRunInterrupted || current.Data.StatusReasonCode != "runner_run_lost" {
		t.Fatalf("lost runner state = %#v", current.Data)
	}
}

func TestMonitorImmediatelyInterruptsRunnerContractMismatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	runner := &runnerStub{now: func() time.Time { return now }}
	service, _ := newTestService(&now, runner, nil, nil)
	created, err := service.Create(t.Context(), model.DemoCreateRunRequest{Scenario: model.DemoScenarioHappy}, "contract-get", "admin")
	if err != nil {
		t.Fatal(err)
	}
	runner.getError = ErrRunnerContract
	if err := service.MonitorOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(t.Context(), created.Data.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Data.Status != model.DemoRunInterrupted || current.Data.StatusReasonCode != "runner_contract_mismatch" {
		t.Fatalf("contract mismatch state = %#v", current.Data)
	}
}

func TestApprovalTimeoutDeniesExactTicketBeforeRunnerCancellation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	order := []string{}
	runner := &runnerStub{now: func() time.Time { return now }, order: &order}
	approvals := &approvalStub{err: errors.New("not configured"), order: &order}
	service, _ := newTestService(&now, runner, approvals, nil)
	created, err := service.Create(t.Context(), model.DemoCreateRunRequest{Scenario: model.DemoScenarioApproval}, "timeout-request", "admin")
	if err != nil {
		t.Fatal(err)
	}
	runner.snapshot.CurrentStep = "guarded_action"
	runner.snapshot.CompletedSteps = 7
	approvals.err = nil
	approvals.approval = model.Approval{
		ProtectResourceBase: model.ProtectResourceBase{
			ID: "ticket-timeout", UpstreamID: "ticket-upstream-timeout", Source: model.SourceAgentGuard,
			FetchedAt: now, RawRef: model.RawRef{Source: model.SourceAgentGuard, ID: "/v1/backend/approvals/0"},
		},
		SessionID: created.Data.SessionID, EventType: "tool_invoke", Phase: "tool_before",
		Action: "human_check", MatchedRules: []string{"demo_tripwire"}, Status: "pending", CreatedAt: now,
	}
	if err := service.MonitorOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if err := service.MonitorOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(t.Context(), created.Data.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if approvals.resolveCalls != 1 || runner.cancelCalls != 1 || current.Data.Status != model.DemoRunExpired ||
		current.Data.Approval == nil || current.Data.Approval.Status != "denied" || len(order) != 2 || order[0] != "deny" || order[1] != "cancel" {
		t.Fatalf("timeout cleanup: approvals=%d cancels=%d order=%v run=%#v", approvals.resolveCalls, runner.cancelCalls, order, current.Data)
	}
}

func TestObservedMetricsRequireExactTraceTaskAndSessionEvidence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	runner := &runnerStub{now: func() time.Time { return now }}
	traces := &traceStub{err: storage.ErrTraceNotFound}
	service, _ := newTestService(&now, runner, nil, traces)
	created, err := service.Create(t.Context(), model.DemoCreateRunRequest{Scenario: model.DemoScenarioHappy}, "trace-request", "admin")
	if err != nil {
		t.Fatal(err)
	}
	const traceID = "11111111111111111111111111111111"
	const spanID = "2222222222222222"
	runner.snapshot.TraceID = traceID
	runner.snapshot.RootSpanID = spanID
	if err := service.MonitorOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	pending, _ := service.Get(t.Context(), created.Data.RunID)
	if pending.Data.ObservedMetrics != nil || pending.Data.Correlations.Trace.Status != "pending" {
		t.Fatalf("missing Trace evidence was treated as observed: %#v", pending.Data)
	}
	traces.err = nil
	traces.summary = telemetry.Summary{TraceID: "33333333333333333333333333333333", TaskID: pending.Data.TaskID, SessionID: pending.Data.SessionID, LLMCalls: 3}
	traceMismatch, _ := service.Get(t.Context(), created.Data.RunID)
	if traceMismatch.Data.ObservedMetrics != nil || traceMismatch.Data.Correlations.Trace.Status != "unavailable" {
		t.Fatalf("mismatched Trace ID was accepted: %#v", traceMismatch.Data)
	}
	traces.summary = telemetry.Summary{TraceID: traceID, TaskID: "wrong", SessionID: pending.Data.SessionID, LLMCalls: 3}
	mismatch, _ := service.Get(t.Context(), created.Data.RunID)
	if mismatch.Data.ObservedMetrics != nil || mismatch.Data.Correlations.Trace.Status != "unavailable" {
		t.Fatalf("mismatched Trace evidence was accepted: %#v", mismatch.Data)
	}
	traces.summary = telemetry.Summary{
		TraceID: traceID, TaskID: pending.Data.TaskID, SessionID: pending.Data.SessionID,
		LLMCalls: 3, MCPCalls: 2, LocalToolCalls: 1, A2ACalls: 1,
	}
	verified, _ := service.Get(t.Context(), created.Data.RunID)
	if verified.Data.ObservedMetrics == nil || verified.Data.ObservedMetrics.LLMCalls != 3 || verified.Data.Correlations.Trace.Status != "verified" ||
		verified.Data.Correlations.GatewayLogs.Status != "unavailable" || verified.Data.Links.GatewayLogs != "" {
		t.Fatalf("exact Trace evidence was not projected: %#v", verified.Data)
	}
	logs := &gatewayLogStub{feed: model.AuditFeed{Status: "available", Events: []model.UnifiedEvent{{
		Source: model.SourceAgentGuard, RawRef: model.RawRef{Source: model.SourceAgentGuard, ID: "wrong-source"},
		Correlation: &model.EventCorrelation{TraceID: traceID},
	}}}}
	service.options.GatewayLogs = logs
	service.options.GatewayConsoleURL = "http://demo-gateway.test:15010"
	wrongSource, _ := service.Get(t.Context(), created.Data.RunID)
	if wrongSource.Data.Correlations.GatewayLogs.Status != "unavailable" || wrongSource.Data.Links.GatewayLogs != "" {
		t.Fatalf("non-gateway log evidence was accepted: %#v", wrongSource.Data)
	}
	logs.feed.Events = append(logs.feed.Events, model.UnifiedEvent{
		Source: model.SourceAgentGateway, RawRef: model.RawRef{Source: model.SourceAgentGateway, ID: "log-demo"},
		Correlation: &model.EventCorrelation{TraceID: traceID},
	})
	incompleteGateway, _ := service.Get(t.Context(), created.Data.RunID)
	if incompleteGateway.Data.Correlations.GatewayLogs.Status != "unavailable" ||
		incompleteGateway.Data.Correlations.GatewayLogs.Basis != "agentgateway_exact_trace_evidence_incomplete" ||
		incompleteGateway.Data.Links.GatewayLogs != "" {
		t.Fatalf("partial gateway evidence was treated as complete: %#v", incompleteGateway.Data)
	}
	logs.feed.Events = append(logs.feed.Events,
		model.UnifiedEvent{
			Source: model.SourceAgentGateway, RawRef: model.RawRef{Source: model.SourceAgentGateway, ID: "log-demo"},
			Correlation: &model.EventCorrelation{TraceID: traceID},
		},
		model.UnifiedEvent{
			Source: model.SourceAgentGateway, RawRef: model.RawRef{Source: model.SourceAgentGateway, ID: "log-demo-b"},
			Correlation: &model.EventCorrelation{TraceID: traceID},
		},
		model.UnifiedEvent{
			Source: model.SourceAgentGateway, RawRef: model.RawRef{Source: model.SourceAgentGateway, ID: "log-demo-c"},
			Correlation: &model.EventCorrelation{TraceID: traceID},
		},
	)
	gatewayVerified, _ := service.Get(t.Context(), created.Data.RunID)
	if gatewayVerified.Data.Correlations.GatewayLogs.Status != "verified" ||
		gatewayVerified.Data.Correlations.GatewayLogs.Basis != "complete_identical_agentgateway_trace_id_set" ||
		gatewayVerified.Data.Links.GatewayLogs != "http://demo-gateway.test:15010/ui/llm/logs?log=log-demo" {
		t.Fatalf("exact gateway evidence was not linked: %#v", gatewayVerified.Data)
	}
	logs.feed.Events = append(logs.feed.Events, model.UnifiedEvent{
		Source: model.SourceAgentGateway, RawRef: model.RawRef{Source: model.SourceAgentGateway, ID: "log-demo-extra"},
		Correlation: &model.EventCorrelation{TraceID: traceID},
	})
	countMismatch, _ := service.Get(t.Context(), created.Data.RunID)
	if countMismatch.Data.Correlations.GatewayLogs.Status != "unavailable" ||
		countMismatch.Data.Correlations.GatewayLogs.Basis != "agentgateway_exact_trace_evidence_count_mismatch" ||
		countMismatch.Data.Links.GatewayLogs != "" {
		t.Fatalf("unexpected gateway evidence count was accepted: %#v", countMismatch.Data)
	}
}
