package guard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

func TestHealthUsesAPIKeyAndVerifiedContract(t *testing.T) {
	t.Parallel()

	const apiKey = "guard-secret-with-enough-entropy"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != apiKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/backend/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"version":"0.3.0","status":"ok","service":"agentguard-server"}`))
	}))
	defer server.Close()

	client, err := New(server.URL, apiKey, server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	health := client.Health(t.Context())
	if health.Status != "healthy" || health.Version != "0.3.0" {
		t.Fatalf("unexpected health: %#v", health)
	}
}

func TestAuditTrafficAndSessionsUseVerifiedRedactedContracts(t *testing.T) {
	t.Parallel()
	const apiKey = "guard-secret-with-enough-entropy"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Api-Key") != apiKey {
			t.Fatal("missing AgentGuard API key")
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/backend/traffic":
			if request.URL.Query().Get("n") != "500" {
				t.Fatalf("unexpected traffic limit: %s", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `[{"ts":1784707200,"tool":"mail.send","agent":"agent-a","session":"session-a","action":"deny","latency_ms":12.5,"risk":0.9,"rules":["rule-a"],"reason":"never-return-reason","plugin_result":{"secret":"never-return-plugin"}}]`)
		case "/v1/backend/audit/recent":
			_, _ = io.WriteString(writer, `[{"event":{"event_id":"event-a","ts_ms":1784707200000,"event_type":"tool_invoke","principal":{"agent_id":"agent-a","session_id":"session-a","user_id":"user-a"},"tool_call":{"tool_name":"mail.send","args":{"secret":"never-return-args"},"result":"never-return-result","source":"langchain","mcp":{"mcp_unique_id":"mcp-a","mcp_name":"mail","mcp_tool_name":"send","mcp_transport":"stdio"}}},"decision":{"action":"deny","risk_score":0.9,"matched_rules":["rule-a"],"rule_version":"1","reason":"never-return-reason","policy_id":"policy-a","plugin_result":{"secret":"never-return-plugin"}},"runtime_state":{"payload":"never-return-runtime"}}]`)
		case "/v1/backend/sessions":
			_, _ = io.WriteString(writer, `{"sessions":[{"session_id":"session-a","agent_id":"agent-a","user_id":"user-a","last_seen":1784707200}]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := New(server.URL, apiKey, server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	traffic, err := client.Traffic(t.Context(), 500)
	if err != nil || len(traffic.Traffic) != 1 || traffic.Traffic[0].Action != "DENY" {
		t.Fatalf("unexpected traffic: %#v err=%v", traffic, err)
	}
	audit, err := client.Audit(t.Context(), 500)
	if err != nil || len(audit.Events) != 1 {
		t.Fatalf("unexpected audit: %#v err=%v", audit, err)
	}
	event := audit.Events[0]
	if event.RawRef != (model.RawRef{Source: model.SourceAgentGuard, ID: "event-a"}) || event.Subject.SessionID != "session-a" || event.Phase != "tool_before" {
		t.Fatalf("source identity was not preserved: %#v", event)
	}
	sessions, err := client.AuditSessions(t.Context())
	if err != nil || len(sessions) != 1 || sessions[0].UpstreamID != "session-a" {
		t.Fatalf("unexpected sessions: %#v err=%v", sessions, err)
	}
	encoded, _ := json.Marshal(struct{ Traffic, Audit any }{traffic, audit})
	for _, forbidden := range []string{"never-return-args", "never-return-result", "never-return-reason", "never-return-plugin", "never-return-runtime"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("audit projection leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestErrorsNeverContainAPIKeyOrResponseBody(t *testing.T) {
	t.Parallel()

	const apiKey = "guard-secret-with-enough-entropy"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("sensitive-upstream-payload"))
	}))
	defer server.Close()

	client, err := New(server.URL, apiKey, server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	health := client.Health(t.Context())
	for _, forbidden := range []string{apiKey, "sensitive-upstream-payload"} {
		if contains(health.Message, forbidden) {
			t.Fatalf("health message leaked %q: %s", forbidden, health.Message)
		}
	}
}

func contains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
