// Package api implements the OpenAPI-defined Phase 2 HTTP surface.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/aggregate"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/auth"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/stream"
)

const maxLoginBodyBytes = 4096

type contextKey string

const requestIDKey contextKey = "request-id"

type ServerConfig struct {
	Sessions    *auth.Manager
	Aggregate   *aggregate.Service
	Stream      *stream.Hub
	Logger      *slog.Logger
	AuthEnabled bool
}

type server struct {
	config ServerConfig
	mux    *http.ServeMux
}

func New(config ServerConfig) http.Handler {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.Stream == nil {
		config.Stream = stream.NewHub()
	}
	service := &server{config: config, mux: http.NewServeMux()}
	service.routes()
	return service.middleware(service.mux)
}

func (server *server) routes() {
	server.mux.HandleFunc("POST /api/v1/auth/session", server.login)
	server.mux.Handle("GET /api/v1/system/health", server.requireAuth(http.HandlerFunc(server.health)))
	server.mux.Handle("GET /api/v1/system/capabilities", server.requireAuth(http.HandlerFunc(server.capabilities)))
	server.mux.Handle("GET /api/v1/overview", server.requireAuth(http.HandlerFunc(server.overview)))
	server.mux.Handle("GET /api/v1/stream", server.requireAuth(http.HandlerFunc(server.eventStream)))
	server.mux.Handle("/api/v1/", server.requireAuth(http.HandlerFunc(server.notImplemented)))
}

func (server *server) login(writer http.ResponseWriter, request *http.Request) {
	if !server.config.AuthEnabled {
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	if server.config.Sessions == nil {
		server.writeError(writer, request, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication is unavailable", nil, true)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxLoginBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Token string `json:"token"`
	}
	if err := decoder.Decode(&input); err != nil || input.Token == "" {
		server.writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "a valid login request is required", nil, false)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		server.writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "a valid login request is required", nil, false)
		return
	}
	csrf, err := server.config.Sessions.Login(writer, input.Token)
	if err != nil {
		server.writeError(writer, request, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid credentials", nil, false)
		return
	}
	writer.Header().Set("X-CSRF-Token", csrf)
	writer.WriteHeader(http.StatusNoContent)
}

func (server *server) health(writer http.ResponseWriter, request *http.Request) {
	server.writeJSON(writer, http.StatusOK, server.config.Aggregate.Health())
}

func (server *server) capabilities(writer http.ResponseWriter, request *http.Request) {
	server.writeJSON(writer, http.StatusOK, server.config.Aggregate.Capabilities(request.Context()))
}

func (server *server) overview(writer http.ResponseWriter, _ *http.Request) {
	server.writeJSON(writer, http.StatusOK, server.config.Aggregate.Overview())
}

func (server *server) eventStream(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		server.writeError(writer, request, http.StatusInternalServerError, "STREAM_UNAVAILABLE", "event streaming is unavailable", nil, true)
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache, no-store")
	writer.Header().Set("Connection", "keep-alive")
	writer.Header().Set("X-Accel-Buffering", "no")

	for _, health := range server.config.Aggregate.Snapshot() {
		if err := writeSSE(writer, healthEvent(health)); err != nil {
			return
		}
	}
	flusher.Flush()

	events, unsubscribe := server.config.Stream.Subscribe()
	defer unsubscribe()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case event, open := <-events:
			if !open || writeSSE(writer, event) != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(writer, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (server *server) notImplemented(writer http.ResponseWriter, request *http.Request) {
	if server.config.AuthEnabled && isWrite(request.Method) {
		session, ok := server.session(request)
		if !ok || request.Header.Get("X-CSRF-Token") == "" || !server.config.Sessions.ValidCSRF(session, request.Header.Get("X-CSRF-Token")) {
			server.writeError(writer, request, http.StatusForbidden, "CSRF_REQUIRED", "a valid CSRF token is required", nil, false)
			return
		}
	}
	server.writeError(writer, request, http.StatusNotImplemented, "PHASE_NOT_IMPLEMENTED", "this operation is reserved for a later integration phase", nil, false)
}

func (server *server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !server.config.AuthEnabled {
			next.ServeHTTP(writer, request)
			return
		}
		if _, ok := server.session(request); !ok {
			server.writeError(writer, request, http.StatusUnauthorized, "AUTH_REQUIRED", "an authenticated admin session is required", nil, false)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (server *server) session(request *http.Request) (auth.Session, bool) {
	if server.config.Sessions == nil {
		return auth.Session{}, false
	}
	return server.config.Sessions.Authenticate(request)
}

func (server *server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestID := newRequestID()
		request = request.WithContext(context.WithValue(request.Context(), requestIDKey, requestID))
		writer.Header().Set("X-Request-ID", requestID)
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Cache-Control", "no-store")
		captured := &statusWriter{ResponseWriter: writer, status: http.StatusOK}
		started := time.Now()
		defer func() {
			if recovered := recover(); recovered != nil {
				server.config.Logger.Error("request panic", "request_id", requestID)
				server.writeError(captured, request, http.StatusInternalServerError, "INTERNAL_ERROR", "the request could not be completed", nil, true)
			}
			server.config.Logger.Info("request completed",
				"request_id", requestID,
				"method", request.Method,
				"path", request.URL.Path,
				"status", captured.status,
				"duration_ms", time.Since(started).Milliseconds(),
			)
		}()
		next.ServeHTTP(captured, request)
	})
}

func (server *server) writeError(writer http.ResponseWriter, request *http.Request, status int, code, message string, source *model.Source, retryable bool) {
	server.writeJSON(writer, status, model.ErrorEnvelope{Error: model.APIError{
		Code: code, Message: message, Source: source, RequestID: requestID(request.Context()), Retryable: retryable,
	}})
}

func (*server) writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}

func writeSSE(writer http.ResponseWriter, event model.UnifiedEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "id: %s\nevent: %s\ndata: %s\n\n", event.ID, event.Kind, payload)
	return err
}

func healthEvent(health model.SourceHealth) model.UnifiedEvent {
	severity := "info"
	if health.Status == model.HealthDown {
		severity = "high"
	} else if health.Status == model.HealthDegraded || health.Status == model.HealthUnknown {
		severity = "medium"
	}
	checkedAt := health.CheckedAt
	if checkedAt.IsZero() {
		checkedAt = time.Now().UTC()
	}
	id := string(health.Source) + "-health-" + checkedAt.Format("20060102T150405.000000000Z")
	return model.UnifiedEvent{
		ID: id, Timestamp: checkedAt, Source: health.Source, Kind: "health", Severity: severity,
		Summary: health.Label + " is " + string(health.Status),
		RawRef:  model.RawRef{Source: health.Source, ID: id},
	}
}

func isWrite(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func newRequestID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}

func requestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *statusWriter) Flush() {
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (writer *statusWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }
