package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/aggregate"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/auth"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/stream"
)

type apiFakeSource struct{ health model.SourceHealth }

func (source apiFakeSource) Health(context.Context) model.SourceHealth { return source.health }
func (apiFakeSource) Capabilities(context.Context) []model.Capability  { return nil }

func TestSessionAuthenticationPartialHealthAndSecretSafeLogging(t *testing.T) {
	t.Parallel()

	const adminToken = "admin-token-with-enough-entropy"
	checkedAt := time.Now().UTC()
	aggregator := aggregate.New("test", apiFakeSource{model.SourceHealth{
		Source: model.SourceAgentGateway, Status: model.HealthHealthy, CheckedAt: checkedAt,
	}}, apiFakeSource{model.SourceHealth{
		Source: model.SourceAgentGuard, Status: model.HealthDown, CheckedAt: checkedAt, Message: "upstream unavailable",
	}})
	aggregator.Refresh(t.Context())

	var logs bytes.Buffer
	handler := New(ServerConfig{
		Sessions:    auth.New(adminToken, auth.Options{TTL: time.Hour}),
		Aggregate:   aggregator,
		Stream:      stream.NewHub(),
		Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
		AuthEnabled: true,
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	unauthorized, err := server.Client().Get(server.URL + "/api/v1/system/health")
	if err != nil {
		t.Fatal(err)
	}
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthorized.StatusCode)
	}
	_ = unauthorized.Body.Close()

	loginBody := strings.NewReader(`{"token":"` + adminToken + `"}`)
	login, err := server.Client().Post(server.URL+"/api/v1/auth/session", "application/json", loginBody)
	if err != nil {
		t.Fatal(err)
	}
	if login.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(login.Body)
		t.Fatalf("login status = %d body=%s", login.StatusCode, body)
	}
	csrf := login.Header.Get("X-CSRF-Token")
	cookies := login.Cookies()
	_ = login.Body.Close()

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/system/health", nil)
	request.AddCookie(cookies[0])
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var envelope model.HealthEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Meta.Partial || len(envelope.Data) != 2 {
		t.Fatalf("unexpected partial health: %#v", envelope)
	}

	writeRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/protect/runtime-rules/check", strings.NewReader(`{}`))
	writeRequest.AddCookie(cookies[0])
	writeWithoutCSRF, err := server.Client().Do(writeRequest)
	if err != nil {
		t.Fatal(err)
	}
	if writeWithoutCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("write without CSRF status = %d", writeWithoutCSRF.StatusCode)
	}
	_ = writeWithoutCSRF.Body.Close()

	writeRequest, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/protect/runtime-rules/check", strings.NewReader(`{}`))
	writeRequest.AddCookie(cookies[0])
	writeRequest.Header.Set("X-CSRF-Token", csrf)
	writeWithCSRF, err := server.Client().Do(writeRequest)
	if err != nil {
		t.Fatal(err)
	}
	if writeWithCSRF.StatusCode != http.StatusNotImplemented {
		t.Fatalf("future write with CSRF status = %d", writeWithCSRF.StatusCode)
	}
	_ = writeWithCSRF.Body.Close()

	if strings.Contains(logs.String(), adminToken) {
		t.Fatalf("access log leaked admin token: %s", logs.String())
	}
}
