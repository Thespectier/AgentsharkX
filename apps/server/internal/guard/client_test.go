package guard

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
