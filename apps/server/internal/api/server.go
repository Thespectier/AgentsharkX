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
	"github.com/Thespectier/AgentsharkX/apps/server/internal/guard"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/protect"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/stream"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/trust"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/upstream"
)

const maxLoginBodyBytes = 4096
const maxMutationBodyBytes = 16 * 1024

type contextKey string

const requestIDKey contextKey = "request-id"

type ServerConfig struct {
	Sessions    *auth.Manager
	Aggregate   *aggregate.Service
	Connect     *connect.Service
	Trust       *trust.Service
	Protect     *protect.Service
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
	server.mux.Handle("GET /api/v1/trust/agents", server.requireAuth(http.HandlerFunc(server.trustAgents)))
	server.mux.Handle("GET /api/v1/trust/agents/{agentId}", server.requireAuth(http.HandlerFunc(server.trustAgent)))
	server.mux.Handle("GET /api/v1/trust/resources", server.requireAuth(http.HandlerFunc(server.trustResources)))
	server.mux.Handle("GET /api/v1/trust/scans", server.requireAuth(http.HandlerFunc(server.trustScans)))
	server.mux.Handle("GET /api/v1/trust/scans/{scanId}", server.requireAuth(http.HandlerFunc(server.trustScan)))
	server.mux.Handle("PATCH /api/v1/trust/agents/{agentId}/tools/{tool}/labels", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.updateToolLabels))))
	server.mux.Handle("POST /api/v1/trust/agents/{agentId}/skills/detect", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.detectSkills))))
	server.mux.Handle("POST /api/v1/trust/agents/{agentId}/mcps/detect", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.detectMCPs))))
	server.mux.Handle("GET /api/v1/protect/policies", server.requireAuth(http.HandlerFunc(server.protectPolicies)))
	server.mux.Handle("POST /api/v1/protect/runtime-rules/check", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.checkRuntimeRule))))
	server.mux.Handle("POST /api/v1/protect/agents/{agentId}/runtime-rules", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.publishRuntimeRule))))
	server.mux.Handle("DELETE /api/v1/protect/agents/{agentId}/runtime-rules/{ruleId}", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.deleteRuntimeRule))))
	server.mux.Handle("GET /api/v1/protect/approvals", server.requireAuth(http.HandlerFunc(server.protectApprovals)))
	server.mux.Handle("POST /api/v1/protect/approvals/{ticketId}/approve", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.approveTicket))))
	server.mux.Handle("POST /api/v1/protect/approvals/{ticketId}/deny", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.denyTicket))))
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

func (server *server) trustAgents(writer http.ResponseWriter, request *http.Request) {
	query, ok := server.resourceQuery(writer, request)
	if !ok || !server.trustAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Trust.Agents(request.Context(), query.search, query.cursor, query.limit)
	server.writeTrustResult(writer, request, http.StatusOK, envelope, err)
}

func (server *server) trustAgent(writer http.ResponseWriter, request *http.Request) {
	if !server.trustAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Trust.Agent(request.Context(), request.PathValue("agentId"))
	server.writeTrustResult(writer, request, http.StatusOK, envelope, err)
}

func (server *server) trustResources(writer http.ResponseWriter, request *http.Request) {
	query, ok := server.resourceQuery(writer, request)
	if !ok || !server.trustAvailable(writer, request) {
		return
	}
	resourceType := strings.TrimSpace(request.URL.Query().Get("type"))
	agentID := strings.TrimSpace(request.URL.Query().Get("agentId"))
	if len(resourceType) > 16 || len(agentID) > 256 {
		server.writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "trust resource query is invalid", source(model.SourceAgentGuard), false)
		return
	}
	envelope, err := server.config.Trust.Resources(request.Context(), query.search, resourceType, agentID, query.cursor, query.limit)
	server.writeTrustResult(writer, request, http.StatusOK, envelope, err)
}

func (server *server) trustScans(writer http.ResponseWriter, request *http.Request) {
	query, ok := server.resourceQuery(writer, request)
	if !ok || !server.trustAvailable(writer, request) {
		return
	}
	if query.search != "" {
		server.writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "scan jobs do not support search", source(model.SourceAgentGuard), false)
		return
	}
	envelope, err := server.config.Trust.ScanJobs(query.cursor, query.limit)
	server.writeTrustResult(writer, request, http.StatusOK, envelope, err)
}

func (server *server) trustScan(writer http.ResponseWriter, request *http.Request) {
	if !server.trustAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Trust.ScanJob(request.PathValue("scanId"))
	server.writeTrustResult(writer, request, http.StatusOK, envelope, err)
}

func (server *server) updateToolLabels(writer http.ResponseWriter, request *http.Request) {
	if !server.trustAvailable(writer, request) {
		return
	}
	var update model.TrustLabelUpdate
	if !server.decodeMutation(writer, request, &update) {
		return
	}
	envelope, err := server.config.Trust.UpdateToolLabels(request.Context(), request.PathValue("agentId"), request.PathValue("tool"), update)
	server.writeTrustResult(writer, request, http.StatusOK, envelope, err)
}

func (server *server) detectSkills(writer http.ResponseWriter, request *http.Request) {
	server.startDetection(writer, request, "skill")
}

func (server *server) detectMCPs(writer http.ResponseWriter, request *http.Request) {
	server.startDetection(writer, request, "mcp")
}

func (server *server) startDetection(writer http.ResponseWriter, request *http.Request, resourceType string) {
	if !server.trustAvailable(writer, request) {
		return
	}
	var input model.TrustDetectionRequest
	if !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Trust.StartScan(request.Context(), request.PathValue("agentId"), resourceType, input)
	server.writeTrustResult(writer, request, http.StatusAccepted, envelope, err)
}

func (server *server) protectPolicies(writer http.ResponseWriter, request *http.Request) {
	if !server.protectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Protect.Snapshot(request.Context())
	server.writeProtectResult(writer, request, http.StatusOK, envelope, err)
}

func (server *server) checkRuntimeRule(writer http.ResponseWriter, request *http.Request) {
	if !server.protectAvailable(writer, request) {
		return
	}
	var input model.RuntimeRuleCheckRequest
	if !server.decodeMutation(writer, request, &input) {
		return
	}
	result, err := server.config.Protect.CheckRule(request.Context(), input.Source)
	if err != nil {
		server.writeProtectResult(writer, request, http.StatusOK, nil, err)
		return
	}
	result.RequestID = requestID(request.Context())
	now := time.Now().UTC()
	server.config.Logger.Info("protect operation completed",
		"request_id", result.RequestID, "operation", "check-runtime-rule", "target", "runtime-rule", "status", "succeeded")
	server.writeJSON(writer, http.StatusOK, model.ResourceEnvelope[model.RuntimeRuleCheck]{
		Data: result, Meta: model.Meta{Source: model.SourceAgentGuard, FetchedAt: now},
	})
}

func (server *server) publishRuntimeRule(writer http.ResponseWriter, request *http.Request) {
	if !server.protectAvailable(writer, request) {
		return
	}
	var input model.RuntimeRulePublishRequest
	if !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Protect.PublishRule(request.Context(), request.PathValue("agentId"), input)
	server.writeProtectMutation(writer, request, http.StatusCreated, envelope, err)
}

func (server *server) deleteRuntimeRule(writer http.ResponseWriter, request *http.Request) {
	if !server.protectAvailable(writer, request) {
		return
	}
	var input model.ConfirmedActionRequest
	if !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Protect.DeleteRule(
		request.Context(), request.PathValue("agentId"), request.PathValue("ruleId"), input,
	)
	server.writeProtectMutation(writer, request, http.StatusOK, envelope, err)
}

func (server *server) protectApprovals(writer http.ResponseWriter, request *http.Request) {
	query, ok := server.resourceQuery(writer, request)
	if !ok || !server.protectAvailable(writer, request) {
		return
	}
	if query.search != "" {
		server.writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "approval queue does not support search", source(model.SourceAgentGuard), false)
		return
	}
	envelope, err := server.config.Protect.Approvals(request.Context(), query.cursor, query.limit)
	server.writeProtectResult(writer, request, http.StatusOK, envelope, err)
}

func (server *server) approveTicket(writer http.ResponseWriter, request *http.Request) {
	server.resolveTicket(writer, request, "approve")
}

func (server *server) denyTicket(writer http.ResponseWriter, request *http.Request) {
	server.resolveTicket(writer, request, "deny")
}

func (server *server) resolveTicket(writer http.ResponseWriter, request *http.Request, decision string) {
	if !server.protectAvailable(writer, request) {
		return
	}
	var input model.ConfirmedActionRequest
	if !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Protect.ResolveApproval(request.Context(), request.PathValue("ticketId"), decision, input)
	server.writeProtectMutation(writer, request, http.StatusOK, envelope, err)
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

func (server *server) trustAvailable(writer http.ResponseWriter, request *http.Request) bool {
	if server.config.Trust != nil {
		return true
	}
	server.writeError(writer, request, http.StatusServiceUnavailable, "TRUST_UNAVAILABLE", "AgentGuard trust integration is unavailable", source(model.SourceAgentGuard), true)
	return false
}

func (server *server) protectAvailable(writer http.ResponseWriter, request *http.Request) bool {
	if server.config.Protect != nil {
		return true
	}
	server.writeError(writer, request, http.StatusServiceUnavailable, "PROTECT_UNAVAILABLE", "Protect integration is unavailable", nil, true)
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

func (server *server) writeTrustResult(writer http.ResponseWriter, request *http.Request, status int, envelope any, err error) {
	if err == nil {
		server.writeJSON(writer, status, envelope)
		return
	}
	if errors.Is(err, trust.ErrInvalidCursor) {
		server.writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "pagination cursor is invalid", source(model.SourceAgentGuard), false)
		return
	}
	if errors.Is(err, trust.ErrInvalidRequest) {
		server.writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "the trust request is invalid", source(model.SourceAgentGuard), false)
		return
	}
	if errors.Is(err, trust.ErrNotFound) {
		server.writeError(writer, request, http.StatusNotFound, "NOT_FOUND", "AgentGuard identity or resource was not found", source(model.SourceAgentGuard), false)
		return
	}
	if errors.Is(err, trust.ErrScanCapacity) {
		server.writeError(writer, request, http.StatusServiceUnavailable, "SCAN_CAPACITY_REACHED", "scan capacity is temporarily exhausted", source(model.SourceAgentGuard), true)
		return
	}
	var contractError *guard.ContractError
	if errors.As(err, &contractError) {
		server.writeError(writer, request, http.StatusBadGateway, "UPSTREAM_CONTRACT_MISMATCH", contractError.Error(), source(model.SourceAgentGuard), false)
		return
	}
	var upstreamError *upstream.Error
	if errors.As(err, &upstreamError) {
		if upstreamError.Status == http.StatusNotFound {
			server.writeError(writer, request, http.StatusNotFound, "NOT_FOUND", "AgentGuard identity or resource was not found", source(model.SourceAgentGuard), false)
			return
		}
		if upstreamError.Status == http.StatusBadRequest || upstreamError.Status == http.StatusUnprocessableEntity {
			server.writeError(writer, request, http.StatusUnprocessableEntity, "UPSTREAM_VALIDATION_FAILED", "AgentGuard rejected the request", source(model.SourceAgentGuard), false)
			return
		}
		server.writeError(writer, request, http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE", "AgentGuard management API is unavailable", source(model.SourceAgentGuard), upstreamError.Retryable)
		return
	}
	server.writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "the request could not be completed", source(model.SourceAgentGuard), true)
}

func (server *server) writeProtectMutation(writer http.ResponseWriter, request *http.Request, status int, envelope model.ProtectMutationEnvelope, err error) {
	if err != nil {
		server.writeProtectResult(writer, request, status, envelope, err)
		return
	}
	envelope.Data.RequestID = requestID(request.Context())
	server.config.Logger.Info("protect operation completed",
		"request_id", envelope.Data.RequestID,
		"operation", envelope.Data.Operation,
		"target", envelope.Data.Target,
		"status", envelope.Data.Status,
		"note_present", true,
	)
	server.writeJSON(writer, status, envelope)
}

func (server *server) writeProtectResult(writer http.ResponseWriter, request *http.Request, status int, envelope any, err error) {
	if err == nil {
		server.writeJSON(writer, status, envelope)
		return
	}
	if errors.Is(err, protect.ErrInvalidCursor) {
		server.writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "pagination cursor is invalid", source(model.SourceAgentGuard), false)
		return
	}
	if errors.Is(err, protect.ErrInvalidRequest) {
		server.writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "the Protect request is invalid", nil, false)
		return
	}
	if errors.Is(err, protect.ErrRuleCheckRequired) {
		server.writeError(writer, request, http.StatusConflict, "RULE_CHECK_REQUIRED", "run a successful syntax check immediately before publishing", source(model.SourceAgentGuard), false)
		return
	}
	if errors.Is(err, protect.ErrMutationInFlight) {
		server.writeError(writer, request, http.StatusConflict, "MUTATION_IN_PROGRESS", "the same Protect action is already in progress", source(model.SourceAgentGuard), true)
		return
	}
	if errors.Is(err, protect.ErrNotFound) {
		server.writeError(writer, request, http.StatusNotFound, "NOT_FOUND", "the rule, agent, or approval ticket is not available", source(model.SourceAgentGuard), false)
		return
	}
	var gatewayContract *gateway.ContractError
	var guardContract *guard.ContractError
	if errors.As(err, &gatewayContract) {
		server.writeError(writer, request, http.StatusBadGateway, "UPSTREAM_CONTRACT_MISMATCH", gatewayContract.Error(), source(model.SourceAgentGateway), false)
		return
	}
	if errors.As(err, &guardContract) {
		server.writeError(writer, request, http.StatusBadGateway, "UPSTREAM_CONTRACT_MISMATCH", guardContract.Error(), source(model.SourceAgentGuard), false)
		return
	}
	var upstreamError *upstream.Error
	if errors.As(err, &upstreamError) {
		if upstreamError.Status == http.StatusNotFound {
			server.writeError(writer, request, http.StatusNotFound, "NOT_FOUND", "the AgentGuard target is no longer pending or available", source(upstreamError.Source), false)
			return
		}
		if upstreamError.Status == http.StatusConflict {
			server.writeError(writer, request, http.StatusConflict, "UPSTREAM_CONFLICT", "AgentGuard reports that the target already exists or changed", source(upstreamError.Source), false)
			return
		}
		if upstreamError.Status == http.StatusBadRequest || upstreamError.Status == http.StatusUnprocessableEntity {
			server.writeError(writer, request, http.StatusUnprocessableEntity, "UPSTREAM_VALIDATION_FAILED", "the upstream source rejected the operation", source(upstreamError.Source), false)
			return
		}
		retryable := upstreamError.Status == 0 || upstreamError.Status == http.StatusRequestTimeout || upstreamError.Status == http.StatusTooManyRequests || upstreamError.Status >= 500
		server.writeError(writer, request, http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE", "the required upstream source is unavailable", source(upstreamError.Source), retryable)
		return
	}
	server.writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "the request could not be completed", nil, true)
}

func (server *server) decodeMutation(writer http.ResponseWriter, request *http.Request, destination any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, maxMutationBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		server.writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "a valid JSON request is required", nil, false)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		server.writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "a single JSON request is required", nil, false)
		return false
	}
	return true
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
	if server.config.AuthEnabled && isWrite(request.Method) && !server.validCSRF(request) {
		server.writeError(writer, request, http.StatusForbidden, "CSRF_REQUIRED", "a valid CSRF token is required", nil, false)
		return
	}
	server.writeError(writer, request, http.StatusNotImplemented, "PHASE_NOT_IMPLEMENTED", "this operation is reserved for a later integration phase", nil, false)
}

func (server *server) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !server.config.AuthEnabled || server.validCSRF(request) {
			next.ServeHTTP(writer, request)
			return
		}
		server.writeError(writer, request, http.StatusForbidden, "CSRF_REQUIRED", "a valid CSRF token is required", nil, false)
	})
}

func (server *server) validCSRF(request *http.Request) bool {
	if server.config.Sessions == nil {
		return false
	}
	session, ok := server.session(request)
	token := request.Header.Get("X-CSRF-Token")
	return ok && token != "" && server.config.Sessions.ValidCSRF(session, token)
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
