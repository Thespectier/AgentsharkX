// Command e2e-upstreams serves contract-shaped fixtures for the release browser test.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type fixtureState struct {
	mu        sync.RWMutex
	emitted   bool
	approved  bool
	emittedAt time.Time
}

func main() {
	state := &fixtureState{}
	gateway := &http.Server{Addr: valueOr("AGENTSHARK_E2E_GATEWAY_ADDR", "127.0.0.1:19000"), Handler: gatewayHandler(state), ReadHeaderTimeout: 3 * time.Second}
	guard := &http.Server{Addr: valueOr("AGENTSHARK_E2E_GUARD_ADDR", "127.0.0.1:19001"), Handler: guardHandler(state), ReadHeaderTimeout: 3 * time.Second}

	errors := make(chan error, 2)
	go func() { errors <- gateway.ListenAndServe() }()
	go func() { errors <- guard.ListenAndServe() }()
	log.Fatal(<-errors)
}

func gatewayHandler(state *fixtureState) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/runtime", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, map[string]any{"build": map[string]string{"version": "1.3.1"}, "ui": map[string]string{"gatewayMode": "standalone"}})
	})
	for _, route := range []string{"GET /config_dump", "GET /api/costs/models"} {
		mux.HandleFunc(route, func(writer http.ResponseWriter, _ *http.Request) { writeJSON(writer, map[string]any{}) })
	}
	mux.HandleFunc("GET /api/config", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, map[string]any{
			"llm": map[string]any{
				"providers": []map[string]any{{"name": "fixture-provider", "provider": "openai"}},
				"models":    []map[string]any{{"name": "fixture-model", "provider": map[string]string{"reference": "fixture-provider"}}},
			},
			"binds": []map[string]any{{"port": 8080, "listeners": []map[string]any{{"name": "fixture-http", "protocol": "HTTP", "routes": []map[string]any{{"name": "fixture-route", "backends": []map[string]string{{"host": "fixture.invalid:443"}}}}}}}},
		})
	})
	mux.HandleFunc("POST /api/logs/analytics/summary", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, map[string]any{"bucketSeconds": 300, "buckets": []any{}})
	})
	mux.HandleFunc("POST /api/logs/search", func(writer http.ResponseWriter, _ *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		logs := []any{}
		if state.emitted {
			logs = append(logs, map[string]any{
				"id": "fixture-request-1", "startedAt": state.emittedAt.Add(-25 * time.Millisecond), "completedAt": state.emittedAt,
				"durationMs": 25, "traceId": "fixture-trace-1", "httpStatus": 200, "error": nil,
				"genAi": map[string]any{"operationName": "chat", "providerName": "fixture-provider", "requestModel": "fixture-model", "responseModel": "fixture-model"},
				"usage": map[string]any{"inputTokens": 4, "outputTokens": 2, "totalTokens": 6}, "hasPayload": false,
			})
		}
		writeJSON(writer, map[string]any{"logs": logs, "nextCursor": nil})
	})
	return mux
}

func guardHandler(state *fixtureState) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /__test/emit", func(writer http.ResponseWriter, _ *http.Request) {
		state.mu.Lock()
		state.emitted = true
		state.approved = false
		state.emittedAt = time.Now().UTC()
		state.mu.Unlock()
		writeJSON(writer, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /v1/backend/health", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, map[string]any{"ok": true, "status": "ok", "version": "0.3.0", "service": "agentguard-server"})
	})
	mux.HandleFunc("GET /v1/backend/sessions", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, map[string]any{"sessions": []map[string]any{{"session_id": "fixture-session", "agent_id": "fixture-agent", "user_id": "fixture-user", "last_seen": float64(time.Now().Unix())}}})
	})
	for _, route := range []string{"GET /v1/backend/tools", "GET /v1/backend/skills", "GET /v1/backend/mcps", "GET /v1/backend/rules", "GET /v1/backend/auditors"} {
		mux.HandleFunc(route, func(writer http.ResponseWriter, _ *http.Request) { writeJSON(writer, []any{}) })
	}
	mux.HandleFunc("GET /v1/backend/traffic", func(writer http.ResponseWriter, _ *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		traffic := []any{}
		if state.emitted {
			traffic = append(traffic, map[string]any{"ts": float64(state.emittedAt.UnixNano()) / float64(time.Second), "action": "human_check", "latency_ms": 7, "risk": 0.8})
		}
		writeJSON(writer, traffic)
	})
	mux.HandleFunc("GET /v1/backend/audit/recent", func(writer http.ResponseWriter, _ *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		events := []any{}
		if state.emitted {
			events = append(events, map[string]any{
				"event": map[string]any{
					"event_id": "fixture-event-1", "ts_ms": state.emittedAt.UnixMilli(), "event_type": "tool_invoke",
					"principal": map[string]string{"agent_id": "fixture-agent", "session_id": "fixture-session", "user_id": "fixture-user"},
					"tool_call": map[string]string{"tool_name": "mail.send", "source": "fixture"},
				},
				"decision": map[string]any{"action": "human_check", "risk_score": 0.8, "matched_rules": []string{"fixture-review"}, "rule_version": "1", "policy_id": "fixture-policy"},
			})
		}
		writeJSON(writer, events)
	})
	mux.HandleFunc("GET /v1/backend/approvals", func(writer http.ResponseWriter, _ *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		approvals := []any{}
		if state.emitted && !state.approved {
			approvals = append(approvals, map[string]any{
				"ticket_id": "fixture-ticket-1", "created_ms": state.emittedAt.UnixMilli(), "status": "pending",
				"event":    map[string]any{"event_id": "fixture-event-1", "event_type": "tool_invoke", "principal": map[string]string{"agent_id": "fixture-agent"}, "tool_call": map[string]string{"tool_name": "mail.send"}},
				"decision": map[string]any{"action": "human_check", "risk_score": 0.8, "matched_rules": []string{"fixture-review"}, "reason": "release fixture"},
			})
		}
		writeJSON(writer, approvals)
	})
	mux.HandleFunc("POST /v1/backend/approvals/fixture-ticket-1/approve", func(writer http.ResponseWriter, _ *http.Request) {
		state.mu.Lock()
		state.approved = true
		state.mu.Unlock()
		writeJSON(writer, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /v1/backend/agents/fixture-agent/plugins/config", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, map[string]any{"agent_id": "fixture-agent", "plugin_config": map[string]any{"phases": map[string]any{}}, "config_source": "server_default"})
	})
	mux.HandleFunc("GET /v1/backend/agents/fixture-agent/plugins/available", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, map[string]any{"agent_id": "fixture-agent", "local_plugins": []any{}, "remote_plugins": []any{}})
	})
	return mux
}

func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func valueOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
