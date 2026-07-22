package upstream

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

func TestGetJSONRetriesOnlyBoundedRetryableFailure(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "sensitive response", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client, err := New(model.SourceAgentGuard, server.URL, "X-Api-Key", "never-log-this-key", server.Client(), 1)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		OK bool `json:"ok"`
	}
	if _, err := client.GetJSON(t.Context(), "/health", &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || requests.Load() != 2 {
		t.Fatalf("unexpected retry result: response=%#v requests=%d", response, requests.Load())
	}
}

func TestGetJSONHonorsClientTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, err := New(model.SourceAgentGateway, server.URL, "", "", &http.Client{Timeout: 10 * time.Millisecond}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetJSON(t.Context(), "/slow", &struct{}{}); err == nil {
		t.Fatal("expected client timeout")
	}
}
