package guard

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/upstream"
)

func TestProtectReadsNormalizeVerifiedFieldsAndDropSensitiveBodies(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/backend/rules":
			_, _ = io.WriteString(writer, `[{"id":"block_shell","name":"block_shell","rule_id":"block_shell","status":"published","tool_pattern":"shell.exec","action":"DENY","severity":"high","category":"boundary","reason":"Shell is not allowed","prompt":"never-return-prompt","pack_id":"agent::agent-a","user_managed":true,"source":"never-return-rule-source"}]`)
		case "/v1/backend/approvals":
			_, _ = io.WriteString(writer, `[{"ticket_id":"ticket-1","created_ms":1784688000000,"status":"pending","event":{"event_id":"event-1","ts_ms":1784688000500,"event_type":"tool_invoke","principal":{"agent_id":"agent-a","session_id":"session-a","user_id":"user-a"},"tool_call":{"tool_name":"send_email","args":{"body":"never-return-approval-body"},"target":{"url":"never-return-target"}}},"decision":{"action":"human_check","risk_score":0.7,"matched_rules":["review_email"],"obligations":["never-return-obligation"],"rule_version":"v1","ttl_ms":0,"reason":"External delivery needs review"}}]`)
		case "/v1/backend/sessions":
			_, _ = io.WriteString(writer, `{"sessions":[{"session_id":"session-a","agent_id":"agent-a","user_id":"user-a","last_seen":1784688000,"client_key":"never-return-session-key"}]}`)
		case "/v1/backend/tools", "/v1/backend/skills", "/v1/backend/mcps":
			_, _ = io.WriteString(writer, `[]`)
		case "/v1/backend/agents/agent-a/plugins/config":
			_, _ = io.WriteString(writer, `{"agent_id":"agent-a","plugin_config":{"phases":{"tool_before":{"client":["tool_invoke"],"server":[{"name":"rule_based_plugin","params":{"secret":"never-return-plugin-param"}}]}}},"config_source":"server_default"}`)
		case "/v1/backend/agents/agent-a/plugins/available":
			_, _ = io.WriteString(writer, `{"agent_id":"agent-a","local_plugins":[{"name":"tool_invoke","description":"safe","event_types":["tool_invoke"],"phases":["tool_before"]}],"remote_plugins":[{"name":"rule_based_plugin","description":"safe","event_types":["tool_invoke"],"phases":["tool_before"]}]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := New(server.URL, "test-key", server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}

	rules, _, err := client.RuntimeRules(t.Context())
	if err != nil || len(rules) != 1 {
		t.Fatalf("unexpected rules: %#v err=%v", rules, err)
	}
	if rules[0].AgentUpstreamID != "agent-a" || rules[0].Action != "DENY" || !rules[0].UserManaged || rules[0].Phase != "unknown" {
		t.Fatalf("unexpected normalized rule: %#v", rules[0])
	}
	approvals, _, err := client.Approvals(t.Context())
	if err != nil || len(approvals) != 1 {
		t.Fatalf("unexpected approvals: %#v err=%v", approvals, err)
	}
	if approvals[0].Tool != "send_email" || approvals[0].Phase != "Tool Before" || approvals[0].AgentUpstreamID != "agent-a" {
		t.Fatalf("unexpected normalized approval: %#v", approvals[0])
	}
	plugins, err := client.ProtectPlugins(t.Context())
	if err != nil || len(plugins.Items) != 4 {
		t.Fatalf("unexpected plugins: %#v err=%v", plugins, err)
	}
	var toolBefore model.ProtectPluginPhase
	for _, phase := range plugins.Items {
		if phase.Phase == "tool_before" {
			toolBefore = phase
		}
	}
	if strings.Join(toolBefore.EnabledLocalPlugins, ",") != "tool_invoke" || strings.Join(toolBefore.EnabledRemotePlugins, ",") != "rule_based_plugin" {
		t.Fatalf("unexpected plugin phase: %#v", toolBefore)
	}

	encoded, err := json.Marshal(struct {
		Rules     []model.RuntimeRule
		Approvals []model.Approval
		Plugins   model.ProtectPluginSnapshot
	}{rules, approvals, plugins})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"never-return", "approval-body", "rule-source", "plugin-param", "session-key", "obligation"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("normalized Protect payload leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestProtectRuleAndApprovalMutationsUseExactNonRetriedContracts(t *testing.T) {
	t.Parallel()
	var publishCalls, deleteCalls, approvalCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(request.Body)
		switch {
		case request.URL.Path == "/v1/backend/rules/check" && request.Method == http.MethodPost:
			if string(body) != `{"source":"RULE: safe\nPOLICY: ALLOW"}` {
				t.Errorf("unexpected check body: %s", body)
			}
			_, _ = io.WriteString(writer, `{"ok":true,"rule_count":1,"errors":[],"warnings":[],"hints":[{"message":"Validated"}],"source_file":"never-return"}`)
		case request.URL.Path == "/v1/backend/agents/agent-a/rules" && request.Method == http.MethodPost:
			publishCalls.Add(1)
			if strings.Contains(string(body), "note") || !strings.Contains(string(body), `"source"`) {
				t.Errorf("unexpected publish body: %s", body)
			}
			_, _ = io.WriteString(writer, `{"ok":true,"agent_id":"agent-a","pack_id":"agent::agent-a","rule_id":"safe","created":true}`)
		case request.URL.Path == "/v1/backend/agents/agent-a/rules/safe" && request.Method == http.MethodDelete:
			deleteCalls.Add(1)
			_, _ = io.WriteString(writer, `{"ok":true,"agent_id":"agent-a","pack_id":"agent::agent-a","rule_id":"safe"}`)
		case request.URL.Path == "/v1/backend/approvals/ticket-1/approve" && request.Method == http.MethodPost:
			approvalCalls.Add(1)
			if string(body) != `{"note":"reviewed"}` {
				t.Errorf("unexpected approval body: %s", body)
			}
			_, _ = io.WriteString(writer, `{"ok":true}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := New(server.URL, "test-key", server.Client(), 3)
	if err != nil {
		t.Fatal(err)
	}

	check, err := client.CheckRuntimeRule(t.Context(), "RULE: safe\nPOLICY: ALLOW")
	if err != nil || !check.OK || check.RuleCount != 1 || len(check.Hints) != 1 {
		t.Fatalf("unexpected rule check: %#v err=%v", check, err)
	}
	if ruleID, err := client.PublishRuntimeRule(t.Context(), "agent-a", "RULE: safe\nPOLICY: ALLOW"); err != nil || ruleID != "safe" {
		t.Fatalf("unexpected publish: rule=%q err=%v", ruleID, err)
	}
	if err := client.DeleteRuntimeRule(t.Context(), "agent-a", "safe"); err != nil {
		t.Fatal(err)
	}
	if err := client.ResolveApproval(t.Context(), "ticket-1", "approve", "reviewed"); err != nil {
		t.Fatal(err)
	}
	if publishCalls.Load() != 1 || deleteCalls.Load() != 1 || approvalCalls.Load() != 1 {
		t.Fatalf("mutations were repeated: publish=%d delete=%d approval=%d", publishCalls.Load(), deleteCalls.Load(), approvalCalls.Load())
	}
}

func TestApprovalMutationDoesNotRetryUpstreamFailure(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(writer, `{"detail":"sensitive upstream body"}`)
	}))
	defer server.Close()
	client, err := New(server.URL, "test-key", server.Client(), 3)
	if err != nil {
		t.Fatal(err)
	}
	err = client.ResolveApproval(context.Background(), "ticket-1", "deny", "reviewed")
	var upstreamError *upstream.Error
	if !errors.As(err, &upstreamError) || upstreamError.Status != http.StatusServiceUnavailable {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls.Load() != 1 || strings.Contains(err.Error(), "sensitive upstream body") {
		t.Fatalf("unsafe retry or error: calls=%d err=%v", calls.Load(), err)
	}
}

func TestRuntimeRuleContractErrorNamesField(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `[{"id":"bad","name":"bad","rule_id":"bad","status":"published","action":"EXECUTE","pack_id":"agent::a","user_managed":true}]`)
	}))
	defer server.Close()
	client, err := New(server.URL, "test-key", server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = client.RuntimeRules(t.Context())
	var contract *ContractError
	if !errors.As(err, &contract) || contract.Field != "/v1/backend/rules/0/action" {
		t.Fatalf("unexpected contract error: %v", err)
	}
}
