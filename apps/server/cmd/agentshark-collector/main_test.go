package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry/receiver"
)

type fakeReadiness struct{ err error }

func (readiness fakeReadiness) Ready(context.Context) error { return readiness.err }

func TestCollectorOperationalEndpointsAndTraceDelegation(t *testing.T) {
	t.Parallel()

	traceCalls := 0
	traceHandler := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		traceCalls++
		response.WriteHeader(http.StatusAccepted)
	})
	metrics := &receiver.Metrics{}
	handler := newCollectorHandler(traceHandler, metrics, fakeReadiness{}, time.Second)

	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d body=%q", path, response.Code, response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(nil))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || traceCalls != 1 {
		t.Fatalf("trace delegation status=%d calls=%d", response.Code, traceCalls)
	}
}

func TestCollectorReadinessIsErrorSafeAndMethodsAreBounded(t *testing.T) {
	t.Parallel()

	handler := newCollectorHandler(http.NotFoundHandler(), &receiver.Metrics{}, fakeReadiness{
		err: errors.New("postgresql://collector:secret@database/traces"),
	}, time.Second)
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("readiness response = %d %q", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/healthz", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("health method response = %d Allow=%q", response.Code, response.Header().Get("Allow"))
	}
}
