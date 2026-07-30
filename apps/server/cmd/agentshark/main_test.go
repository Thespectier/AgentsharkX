package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

type retryingHealthRecorder struct {
	failuresRemaining int
	attempts          []model.SourceHealth
}

func TestDemoModelProbeUsesExactOpenAIModelsEndpoint(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Errorf("probe path = %q", request.URL.Path)
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write([]byte(`{"object":"list","data":[{"id":"agentshark-demo-model-v1","object":"model"}]}`))
	}))
	defer server.Close()
	if err := demoModelProbe(server.URL+"/v1", "agentshark-demo-model-v1")(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := demoModelProbe(server.URL+"/v1", "other-model")(t.Context()); err == nil {
		t.Fatal("probe accepted a response without the configured deterministic model")
	}
}

func (recorder *retryingHealthRecorder) RecordHealth(_ context.Context, health model.SourceHealth) error {
	recorder.attempts = append(recorder.attempts, health)
	if recorder.failuresRemaining > 0 {
		recorder.failuresRemaining--
		return errors.New("database unavailable")
	}
	return nil
}

func TestHealthPersistenceRetriesOriginalTransitionBeforeCurrentState(t *testing.T) {
	t.Parallel()
	initialAt := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	downAt := initialAt.Add(time.Minute)
	recoveredAt := downAt.Add(time.Minute)
	state := newHealthPersistenceState([]model.SourceHealth{{
		Source: model.SourceAgentGateway, Status: model.HealthHealthy, Version: "1.0.0", CheckedAt: initialAt,
	}})
	recorder := &retryingHealthRecorder{failuresRemaining: 1}

	failures := state.persistChanges(t.Context(), recorder, []model.SourceHealth{{
		Source: model.SourceAgentGateway, Status: model.HealthDown, Version: "1.0.0", CheckedAt: downAt,
	}})
	if len(failures) != 1 || len(recorder.attempts) != 1 {
		t.Fatalf("first transition = failures %#v attempts %#v", failures, recorder.attempts)
	}
	failures = state.persistChanges(t.Context(), recorder, []model.SourceHealth{{
		Source: model.SourceAgentGateway, Status: model.HealthHealthy, Version: "1.0.0", CheckedAt: recoveredAt,
	}})
	if len(failures) != 0 || len(recorder.attempts) != 3 {
		t.Fatalf("recovered transitions = failures %#v attempts %#v", failures, recorder.attempts)
	}
	if !recorder.attempts[1].CheckedAt.Equal(downAt) || recorder.attempts[1].Status != model.HealthDown ||
		!recorder.attempts[2].CheckedAt.Equal(recoveredAt) || recorder.attempts[2].Status != model.HealthHealthy {
		t.Fatalf("transition order or timestamps changed: %#v", recorder.attempts)
	}
}
