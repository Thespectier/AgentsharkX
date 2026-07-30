package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/demo"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage/memory"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/stream"
)

type apiDemoRunner struct {
	now        time.Time
	request    demo.RunnerStartRequest
	snapshot   demo.RunnerSnapshot
	startCalls int
}

func (runner *apiDemoRunner) Health(context.Context) (demo.RunnerHealth, error) {
	var active *string
	if runner.request.RunID != "" && runner.snapshot.Status != "succeeded" && runner.snapshot.Status != "failed" && runner.snapshot.Status != "cancelled" {
		value := runner.request.RunID
		active = &value
	}
	return demo.RunnerHealth{Status: "healthy", Service: "agentshark-demo-runner", MaxConcurrency: 1, ActiveRunID: active}, nil
}

func (runner *apiDemoRunner) Start(_ context.Context, request demo.RunnerStartRequest) (demo.RunnerSnapshot, error) {
	runner.request = request
	runner.startCalls++
	totalSteps := 9
	if request.Scenario == model.DemoScenarioApproval {
		totalSteps = 10
	}
	runner.snapshot = demo.RunnerSnapshot{
		RunID: request.RunID, Scenario: request.Scenario, Status: "running", Outcome: model.DemoOutcomeNone,
		DelayMS: request.DelayMS, TaskID: request.TaskID, SessionID: request.SessionID,
		RequestID: request.RequestID, CurrentStep: "bootstrap", TotalSteps: totalSteps,
		StartedAt: &runner.now, HeartbeatAt: runner.now,
	}
	return runner.snapshot, nil
}

func (runner *apiDemoRunner) Get(context.Context, string) (demo.RunnerSnapshot, error) {
	return runner.snapshot, nil
}

func (runner *apiDemoRunner) Cancel(context.Context, string) (demo.RunnerSnapshot, error) {
	runner.snapshot.Status = "cancelled"
	runner.snapshot.Outcome = model.DemoOutcomeCancelled
	runner.snapshot.CompletedAt = &runner.now
	return runner.snapshot, nil
}

func TestDemoStatusIsReadableWhenDisabled(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	store := memory.New(memory.Options{Now: func() time.Time { return now }})
	service := demo.New(store, store, nil, nil, nil, stream.NewHub(), demo.Options{Enabled: false, Now: func() time.Time { return now }})
	handler := New(ServerConfig{Demo: service, Logger: slog.New(slog.DiscardHandler), AuthEnabled: false})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/demo/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"enabled":false`) {
		t.Fatalf("disabled status = %d %s", response.Code, response.Body.String())
	}
}

func TestDemoRunStreamStartsWithSnapshotAndResetsRetainedCursor(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	store := memory.New(memory.Options{Now: func() time.Time { return now }, OutboxRetention: time.Hour})
	hub := stream.NewHub()
	run := model.DemoRun{
		RunID: "11111111-1111-4111-8111-111111111111", Scenario: model.DemoScenarioHappy,
		Status: model.DemoRunRunning, Outcome: model.DemoOutcomeNone, RequestedAt: now,
		TaskID: "demo-task", SessionID: "demo-session", RootAgentID: "demo-incident-investigator",
		TotalSteps: 9, FixtureVersion: "v1", RequestID: "request",
	}
	if _, _, err := store.CreateDemoRun(t.Context(), run, model.DemoRunEvent{
		RunID: run.RunID, Type: "run.status", Status: run.Status, OccurredAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	service := demo.New(store, store, nil, nil, nil, hub, demo.Options{Enabled: false, Now: func() time.Time { return now }})
	handler := New(ServerConfig{Demo: service, Stream: hub, Logger: slog.New(slog.DiscardHandler), AuthEnabled: false})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/demo/runs/"+run.RunID+"/events", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: snapshot") || !strings.Contains(response.Body.String(), run.RunID) {
		t.Fatalf("fresh stream = %d %s", response.Code, response.Body.String())
	}

	ctx, cancel = context.WithCancel(t.Context())
	cancel()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/demo/runs/"+run.RunID+"/events", nil).WithContext(ctx)
	request.Header.Set("Last-Event-ID", "0")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body, _ := io.ReadAll(response.Result().Body)
	if response.Code != http.StatusOK || !strings.Contains(string(body), "event: reset") || !strings.Contains(string(body), "outbox_retention") {
		t.Fatalf("resumed stream = %d %s", response.Code, body)
	}
}

func TestCreateDemoRunUsesRequestIDForHTTPIdempotency(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	store := memory.New(memory.Options{Now: func() time.Time { return now }, OutboxRetention: time.Hour})
	runner := &apiDemoRunner{now: now}
	hub := stream.NewHub()
	service := demo.New(store, store, runner, nil, nil, hub, demo.Options{Enabled: true, Now: func() time.Time { return now }})
	handler := New(ServerConfig{Demo: service, Stream: hub, Logger: slog.New(slog.DiscardHandler), AuthEnabled: false})

	create := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/demo/runs", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Request-ID", "stable-demo-request")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	first := create(`{"scenario":"happy","delayMs":0}`)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first create = %d %s", first.Code, first.Body.String())
	}
	var firstEnvelope model.DemoRunEnvelope
	if err := json.Unmarshal(first.Body.Bytes(), &firstEnvelope); err != nil {
		t.Fatal(err)
	}
	retry := create(`{"scenario":"happy","delayMs":0}`)
	var retryEnvelope model.DemoRunEnvelope
	if err := json.Unmarshal(retry.Body.Bytes(), &retryEnvelope); err != nil {
		t.Fatal(err)
	}
	if retry.Code != http.StatusAccepted || retryEnvelope.Data.RunID != firstEnvelope.Data.RunID || runner.startCalls != 1 {
		t.Fatalf("idempotent retry = %d %#v calls=%d", retry.Code, retryEnvelope, runner.startCalls)
	}
	changed := create(`{"scenario":"failure","delayMs":0}`)
	if changed.Code != http.StatusConflict || !strings.Contains(changed.Body.String(), `"code":"DEMO_RUN_STATE_CHANGED"`) {
		t.Fatalf("changed idempotency payload = %d %s", changed.Code, changed.Body.String())
	}
}

func TestCreateDemoRunNotReadyReturnsFailedRequiredChecks(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	store := memory.New(memory.Options{Now: func() time.Time { return now }, OutboxRetention: time.Hour})
	service := demo.New(store, store, nil, nil, nil, stream.NewHub(), demo.Options{
		Enabled: true, Now: func() time.Time { return now },
	})
	handler := New(ServerConfig{Demo: service, Logger: slog.New(slog.DiscardHandler), AuthEnabled: false})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/demo/runs", strings.NewReader(`{"scenario":"happy","delayMs":0}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "not-ready-request")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("not-ready create = %d %s", response.Code, response.Body.String())
	}
	var failure model.ErrorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Error.Code != "DEMO_NOT_READY" || !failure.Error.Retryable ||
		len(failure.Error.FailedChecks) != 1 || failure.Error.FailedChecks[0].ID != "demo-runner" ||
		!failure.Error.FailedChecks[0].Required {
		t.Fatalf("not-ready failure = %#v", failure)
	}
}
