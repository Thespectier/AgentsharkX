package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesSPAAndPreservesAPIRoutes(t *testing.T) {
	t.Parallel()

	api := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusAccepted)
		_, _ = writer.Write([]byte(request.URL.Path))
	})
	handler := New(api)

	for _, route := range []string{"/api/v1/system/health", "/healthz"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
		if response.Code != http.StatusAccepted || response.Body.String() != route {
			t.Fatalf("API route %q was not preserved: status=%d body=%q", route, response.Code, response.Body.String())
		}
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/audit/security-events", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "AgentsharkX") {
		t.Fatalf("SPA fallback unavailable: status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestHandlerDoesNotTurnUnknownWritesIntoSPAResponses(t *testing.T) {
	t.Parallel()

	handler := New(http.NotFoundHandler())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/unknown", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown write status = %d", response.Code)
	}
}
