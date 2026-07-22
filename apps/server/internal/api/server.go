// Package api implements the OpenAPI-defined management HTTP surface.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/aggregate"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/auth"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/connect"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/gateway"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/stream"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/upstream"
)

const maxLoginBodyBytes = 4096

type contextKey string

const requestIDKey contextKey = "request-id"

type ServerConfig struct {
	Sessions    *auth.Manager
	Aggregate   *aggregate.Service
	Connect     *connect.Service
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
	server.mux.Handle("GET /api/v1/connect/summary", server.requireAuth(http.HandlerFunc(server.connectSummary)))
	server.mux.Handle("GET /api/v1/connect/analytics", server.requireAuth(http.HandlerFunc(server.connectAnalytics)))
	server.mux.Handle("GET /api/v1/connect/setup", server.requireAuth(http.HandlerFunc(server.connectSetup)))
	server.mux.Handle("GET /api/v1/connect/llm/providers", server.requireAuth(http.HandlerFunc(server.providers)))
	server.mux.Handle("GET /api/v1/connect/llm/providers/{resourceId}", server.requireAuth(http.HandlerFunc(server.provider)))
	server.mux.Handle("GET /api/v1/connect/llm/models", server.requireAuth(http.HandlerFunc(server.models)))
	server.mux.Handle("GET /api/v1/connect/llm/models/{resourceId}", server.requireAuth(http.HandlerFunc(server.gatewayModel)))
	server.mux.Handle("GET /api/v1/connect/mcp/servers", server.requireAuth(http.HandlerFunc(server.mcpServers)))
	server.mux.Handle("GET /api/v1/connect/mcp/servers/{resourceId}", server.requireAuth(http.HandlerFunc(server.mcpServer)))
	server.mux.Handle("GET /api/v1/connect/traffic/routes", server.requireAuth(http.HandlerFunc(server.trafficRoutes)))
	server.mux.Handle("GET /api/v1/connect/traffic/routes/{resourceId}", server.requireAuth(http.HandlerFunc(server.route)))
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

func (server *server) connectSummary(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Connect.Summary(request.Context())
	server.writeConnectResult(writer, request, envelope, err)
}

func (server *server) connectAnalytics(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Connect.Analytics(request.Context())
	server.writeConnectResult(writer, request, envelope, err)
}

func (server *server) connectSetup(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	server.writeJSON(writer, http.StatusOK, server.config.Connect.Setup(request.Context()))
}

func (server *server) providers(writer http.ResponseWriter, request *http.Request) {
	query, ok := server.resourceQuery(writer, request)
	if !ok || !server.connectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Connect.Providers(request.Context(), query.search, query.cursor, query.limit)
	server.writeConnectResult(writer, request, envelope, err)
}

func (server *server) provider(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Connect.Provider(request.Context(), request.PathValue("resourceId"))
	server.writeConnectResult(writer, request, envelope, err)
}

func (server *server) models(writer http.ResponseWriter, request *http.Request) {
	query, ok := server.resourceQuery(writer, request)
	if !ok || !server.connectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Connect.Models(request.Context(), query.search, query.cursor, query.limit)
	server.writeConnectResult(writer, request, envelope, err)
}

func (server *server) gatewayModel(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Connect.Model(request.Context(), request.PathValue("resourceId"))
	server.writeConnectResult(writer, request, envelope, err)
}

func (server *server) mcpServers(writer http.ResponseWriter, request *http.Request) {
	query, ok := server.resourceQuery(writer, request)
	if !ok || !server.connectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Connect.MCPServers(request.Context(), query.search, query.cursor, query.limit)
	server.writeConnectResult(writer, request, envelope, err)
}

func (server *server) mcpServer(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Connect.MCPServer(request.Context(), request.PathValue("resourceId"))
	server.writeConnectResult(writer, request, envelope, err)
}

func (server *server) trafficRoutes(writer http.ResponseWriter, request *http.Request) {
	query, ok := server.resourceQuery(writer, request)
	if !ok || !server.connectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Connect.Routes(request.Context(), query.search, query.cursor, query.limit)
	server.writeConnectResult(writer, request, envelope, err)
}

func (server *server) route(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Connect.Route(request.Context(), request.PathValue("resourceId"))
	server.writeConnectResult(writer, request, envelope, err)
}

type listQuery struct {
	search string
	cursor string
	limit  int
}

func (server *server) resourceQuery(writer http.ResponseWriter, request *http.Request) (listQuery, bool) {
	query := listQuery{search: strings.TrimSpace(request.URL.Query().Get("q")), cursor: request.URL.Query().Get("cursor"), limit: 25}
	if len(query.search) > 200 || len(query.cursor) > 256 {
		server.writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "resource query is too long", nil, false)
		return listQuery{}, false
	}
	if rawLimit := request.URL.Query().Get("limit"); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit < 1 || limit > 100 {
			server.writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "limit must be between 1 and 100", nil, false)
			return listQuery{}, false
		}
		query.limit = limit
	}
	return query, true
}

func (server *server) connectAvailable(writer http.ResponseWriter, request *http.Request) bool {
	if server.config.Connect != nil {
		return true
	}
	server.writeError(writer, request, http.StatusServiceUnavailable, "CONNECT_UNAVAILABLE", "agentgateway integration is unavailable", source(model.SourceAgentGateway), true)
	return false
}

func (server *server) writeConnectResult(writer http.ResponseWriter, request *http.Request, envelope any, err error) {
	if err == nil {
		server.writeJSON(writer, http.StatusOK, envelope)
		return
	}
	if errors.Is(err, connect.ErrInvalidCursor) {
		server.writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "pagination cursor is invalid", source(model.SourceAgentGateway), false)
		return
	}
	if errors.Is(err, connect.ErrNotFound) {
		server.writeError(writer, request, http.StatusNotFound, "NOT_FOUND", "agentgateway resource was not found", source(model.SourceAgentGateway), false)
		return
	}
	var contractError *gateway.ContractError
	if errors.As(err, &contractError) {
		server.writeError(writer, request, http.StatusBadGateway, "UPSTREAM_CONTRACT_MISMATCH", contractError.Error(), source(model.SourceAgentGateway), false)
		return
	}
	var upstreamError *upstream.Error
	if errors.As(err, &upstreamError) {
		server.writeError(writer, request, http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE", "agentgateway management API is unavailable", source(model.SourceAgentGateway), upstreamError.Retryable)
		return
	}
	server.writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "the request could not be completed", source(model.SourceAgentGateway), true)
}

func source(value model.Source) *model.Source { return &value }

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
