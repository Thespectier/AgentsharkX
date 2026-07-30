package demo

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRunnerClientUsesBearerAuthAndExactProtocol(t *testing.T) {
	t.Parallel()
	const token = "0123456789abcdef0123456789abcdef"
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/healthz":
			_ = json.NewEncoder(writer).Encode(RunnerHealth{Status: "healthy", Service: "agentshark-demo-runner", MaxConcurrency: 1})
		case "/internal/v1/runs":
			if request.Header.Get("Authorization") != "Bearer "+token {
				t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
			}
			var input RunnerStartRequest
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			writer.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(writer).Encode(RunnerSnapshot{
				RunID: input.RunID, Scenario: input.Scenario, Status: "running", Outcome: "none",
				DelayMS: input.DelayMS, TaskID: input.TaskID, SessionID: input.SessionID,
				RequestID: input.RequestID, TotalSteps: 9, HeartbeatAt: now,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := NewRunnerClient(server.URL, token, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Health(t.Context()); err != nil {
		t.Fatal(err)
	}
	request := RunnerStartRequest{
		RunID: "11111111-1111-4111-8111-111111111111", Scenario: "happy", DelayMS: 0,
		TaskID: "task", SessionID: "session", RequestID: "request",
	}
	snapshot, err := client.Start(t.Context(), request)
	if err != nil || snapshot.RunID != request.RunID || snapshot.HeartbeatAt != now {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
}

func TestRunnerClientMapsFiniteErrorsWithoutReturningBody(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusConflict)
		_, _ = writer.Write([]byte(`{"code":"demo_run_busy","secret":"must-not-propagate"}`))
	}))
	defer server.Close()
	client, err := NewRunnerClient(server.URL, "0123456789abcdef0123456789abcdef", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Start(t.Context(), RunnerStartRequest{})
	if !errors.Is(err, ErrRunnerBusy) || err.Error() != ErrRunnerBusy.Error() {
		t.Fatalf("runner error = %v", err)
	}
}
