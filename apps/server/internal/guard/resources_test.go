package guard

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

func TestTrustSnapshotNormalizesOnlySafeVerifiedFields(t *testing.T) {
	t.Parallel()

	const apiKey = "guard-secret-with-enough-entropy"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Api-Key") != apiKey {
			t.Fatalf("missing API key")
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/backend/sessions":
			_, _ = io.WriteString(writer, `{"sessions":[{"session_id":"s-1","agent_id":"agent-a","user_id":"user-a","last_seen":1784688000.25,"client_key":"never-return","metadata":{"authorization":"never-return"}}]}`)
		case "/v1/backend/tools":
			_, _ = io.WriteString(writer, `[{"owner_agent_id":"agent-a","name":"mail.send","labels":{"boundary":"external","sensitivity":"confidential","integrity":"trusted","tags":["write"]},"input_params":["body"]}]`)
		case "/v1/backend/skills":
			_, _ = io.WriteString(writer, `[{"owner_agent_id":"agent-a","agent_id":"agent-a","session_id":"s-1","skill_unique_id":"skill-1","name":"research","description":"Explicit description","source_framework":"langchain","detect_result":{"object_id":"skill-1","name":"research","label":"suspicious","risk_level":"medium","capabilities":["network"],"risk_labels":["external_io"],"policy_targets":["tool_before"],"suggested_plugins":["policy"],"metadata":{"raw_prompt":"never-return"}},"descriptor":{"files":[{"content":"never-return"}]}}]`)
		case "/v1/backend/mcps":
			_, _ = io.WriteString(writer, `[{"owner_agent_id":"agent-a","agent_id":"agent-a","session_id":"s-1","mcp_unique_id":"mcp-1","name":"inventory","description":"Inventory MCP","source_framework":"mcp_native","transport":"stdio","remote":false,"tool_count":3,"url":"https://secret.example?api_key=never-return","detect_result":null}]`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := New(server.URL, apiKey, server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := client.TrustSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Sessions) != 1 || len(snapshot.Resources) != 3 || len(snapshot.Failures) != 0 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if snapshot.Sessions[0].AgentUpstreamID != "agent-a" || snapshot.Sessions[0].LastSeen == nil {
		t.Fatalf("unexpected session: %#v", snapshot.Sessions[0])
	}
	if snapshot.Resources[0].Labels == nil || snapshot.Resources[0].Labels.Boundary != "external" {
		t.Fatalf("unexpected tool: %#v", snapshot.Resources[0])
	}
	if snapshot.Resources[1].Detection == nil || snapshot.Resources[1].Detection.Label != "suspicious" {
		t.Fatalf("unexpected skill detection: %#v", snapshot.Resources[1])
	}
	if snapshot.Resources[2].Remote == nil || *snapshot.Resources[2].Remote || snapshot.Resources[2].ToolCount == nil || *snapshot.Resources[2].ToolCount != 3 {
		t.Fatalf("unexpected MCP: %#v", snapshot.Resources[2])
	}
	encoded, _ := json.Marshal(snapshot)
	for _, forbidden := range []string{"never-return", "client_key", "authorization", "raw_prompt", "descriptor", "api_key"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("snapshot leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestTrustSnapshotPreservesPartialResourceFailures(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1/backend/mcps" {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/v1/backend/sessions" {
			_, _ = io.WriteString(writer, `{"sessions":[]}`)
			return
		}
		_, _ = io.WriteString(writer, `[]`)
	}))
	defer server.Close()
	client, err := New(server.URL, "secret", server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.TrustSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Failures) != 1 || snapshot.Failures[0].Code != "UPSTREAM_UNAVAILABLE" || !strings.Contains(snapshot.Failures[0].Message, "mcps") {
		t.Fatalf("unexpected failures: %#v", snapshot.Failures)
	}
}

func TestTrustSnapshotReportsFieldScopedContractChanges(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/backend/sessions":
			_, _ = io.WriteString(writer, `{"sessions":[]}`)
		case "/v1/backend/tools":
			_, _ = io.WriteString(writer, `[{"owner_agent_id":"agent-a","labels":{}}]`)
		default:
			_, _ = io.WriteString(writer, `[]`)
		}
	}))
	defer server.Close()
	client, err := New(server.URL, "secret", server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.TrustSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Failures) != 1 || !strings.Contains(snapshot.Failures[0].Message, "/v1/backend/tools/0/name") {
		t.Fatalf("expected field-scoped failure, got %#v", snapshot.Failures)
	}
}

func TestToolLabelsUseExactNonRetriedPatchAndServerResponse(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method != http.MethodPatch || request.URL.EscapedPath() != "/v1/backend/agents/agent%20a/tools/mail.send/labels" {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.EscapedPath())
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"boundary":"external","tags":["approved"]}` {
			t.Fatalf("unexpected body: %s", body)
		}
		_, _ = io.WriteString(writer, `{"ok":true,"tool":{"owner_agent_id":"agent a","name":"mail.send","labels":{"boundary":"server-confirmed","sensitivity":"low","integrity":"trusted","tags":["approved"]}}}`)
	}))
	defer server.Close()
	client, err := New(server.URL, "secret", server.Client(), 3)
	if err != nil {
		t.Fatal(err)
	}
	boundary := "external"
	tags := []string{"approved"}
	updated, err := client.UpdateToolLabels(t.Context(), "agent a", "mail.send", model.TrustLabelUpdate{Boundary: &boundary, Tags: &tags})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || updated.Labels == nil || updated.Labels.Boundary != "server-confirmed" {
		t.Fatalf("mutation did not trust the server response: requests=%d resource=%#v", requests, updated)
	}
}

func TestDetectionUsesExactBodiesAndNormalizesSafeResults(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		switch request.URL.Path {
		case "/v1/backend/agents/agent-a/skills/detect":
			if string(body) != `{"skill_unique_ids":["skill-1"],"use_llm":true}` {
				t.Fatalf("unexpected skill body: %s", body)
			}
			_, _ = io.WriteString(writer, `{"ok":true,"agent_id":"agent-a","requested":1,"detected":1,"missing_skill_unique_ids":[],"results":[{"skill_unique_id":"skill-1","name":"research","detect_result":{"object_id":"skill-1","name":"research","label":"benign","risk_level":"low","capabilities":[],"risk_labels":[],"policy_targets":[],"suggested_plugins":[],"metadata":{"secret":"never-return"}}}]}`)
		case "/v1/backend/agents/agent-a/mcps/detect":
			if string(body) != `{"mcp_unique_ids":["mcp-1"]}` {
				t.Fatalf("unexpected MCP body: %s", body)
			}
			_, _ = io.WriteString(writer, `{"ok":true,"agent_id":"agent-a","requested":1,"detected":1,"missing_mcp_unique_ids":[],"results":[{"mcp_unique_id":"mcp-1","name":"inventory","detect_result":{"object_id":"mcp-1","name":"inventory","label":"suspicious","risk_level":"medium","capabilities":["tools"],"risk_labels":[],"policy_targets":[],"suggested_plugins":[]}}]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := NewWithOperationClient(server.URL, "secret", "v2.1", server.Client(), &http.Client{Timeout: time.Second}, 2)
	if err != nil {
		t.Fatal(err)
	}
	skills, warnings, err := client.DetectSkills(t.Context(), "agent-a", []string{"skill-1"}, true)
	if err != nil || len(warnings) != 0 || len(skills) != 1 || skills[0].Label != "benign" {
		t.Fatalf("unexpected skill result: %#v warnings=%#v err=%v", skills, warnings, err)
	}
	mcps, warnings, err := client.DetectMCPs(t.Context(), "agent-a", []string{"mcp-1"})
	if err != nil || len(warnings) != 0 || len(mcps) != 1 || mcps[0].ResourceUpstreamID != "mcp-1" {
		t.Fatalf("unexpected MCP result: %#v warnings=%#v err=%v", mcps, warnings, err)
	}
	encoded, _ := json.Marshal([]any{skills, mcps})
	if strings.Contains(string(encoded), "never-return") || strings.Contains(string(encoded), "metadata") {
		t.Fatalf("detection leaked raw metadata: %s", encoded)
	}
}

func TestAllTrustRoutesFailAsOneSafeError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(writer, "sensitive response")
	}))
	defer server.Close()
	client, err := New(server.URL, "secret", server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.TrustSnapshot(context.Background())
	if err == nil || strings.Contains(err.Error(), "sensitive response") {
		t.Fatalf("expected safe aggregate failure, got %v", err)
	}
	var contractError *ContractError
	if errors.As(err, &contractError) {
		t.Fatalf("unexpected contract error: %v", err)
	}
}
