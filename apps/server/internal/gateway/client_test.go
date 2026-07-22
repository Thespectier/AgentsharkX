package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

func TestHealthUsesVerifiedRuntimeContract(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runtime" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"build":{"version":"1.3.1","gitRevision":"dbaaf7ed73671e7aec9195e35e7f726c0b14b84a"},"ui":{"gatewayMode":"standalone"}}`))
	}))
	defer server.Close()

	client, err := New(server.URL, server.Client(), 0)
	if err != nil {
		t.Fatal(err)
	}
	health := client.Health(t.Context())
	if health.Source != model.SourceAgentGateway || health.Status != model.HealthHealthy || health.Version != "1.3.1" {
		t.Fatalf("unexpected health: %#v", health)
	}
}

func TestCapabilitiesAreIndependentlyProbed(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/runtime", "/api/config", "/api/costs/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		case "/config_dump":
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, &http.Client{Timeout: time.Second}, 0)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := client.Capabilities(t.Context())
	statuses := make(map[string]model.CapabilityStatus, len(capabilities))
	for _, capability := range capabilities {
		statuses[capability.ID] = capability.Status
	}
	if statuses["gateway.runtime"] != model.CapabilitySupported {
		t.Fatalf("runtime status = %q", statuses["gateway.runtime"])
	}
	if statuses["gateway.configuration"] != model.CapabilityPartial {
		t.Fatalf("configuration status = %q", statuses["gateway.configuration"])
	}
	if statuses["gateway.cost-catalog"] != model.CapabilitySupported {
		t.Fatalf("cost catalog status = %q", statuses["gateway.cost-catalog"])
	}
}
