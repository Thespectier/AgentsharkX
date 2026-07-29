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
	"github.com/Thespectier/AgentsharkX/apps/server/internal/audit"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/auth"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/connect"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/gateway"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/guard"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/protect"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/storage"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/stream"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/trust"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/upstream"
)

const maxLoginBodyBytes = 4096
const maxMutationBodyBytes = 1 << 20

type contextKey string

const requestIDKey contextKey = "request-id"

type ServerConfig struct {
	Sessions    *auth.Manager
	Aggregate   *aggregate.Service
	Connect     *connect.Service
	Trust       *trust.Service
	Protect     *protect.Service
	Audit       *audit.Service
	Stream      *stream.Hub
	Outbox      storage.OutboxStore
	Readiness   storage.Readiness
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
	if config.Outbox == nil && config.Audit != nil {
		config.Outbox = config.Audit.Outbox()
	}
	if config.Readiness == nil && config.Audit != nil {
		config.Readiness = config.Audit
	}
	service := &server{config: config, mux: http.NewServeMux()}
	service.routes()
	return service.middleware(service.mux)
}

func (server *server) routes() {
	server.mux.HandleFunc("GET /healthz", server.liveness)
	server.mux.HandleFunc("GET /readyz", server.readiness)
	server.mux.HandleFunc("POST /api/v1/auth/session", server.login)
	server.mux.Handle("GET /api/v1/auth/session", server.requireAuth(http.HandlerFunc(server.sessionInfo)))
	server.mux.Handle("GET /api/v1/system/health", server.requireAuth(http.HandlerFunc(server.health)))
	server.mux.Handle("GET /api/v1/system/capabilities", server.requireAuth(http.HandlerFunc(server.capabilities)))
	server.mux.Handle("GET /api/v1/system/diagnostics", server.requireAuth(http.HandlerFunc(server.diagnostics)))
	server.mux.Handle("GET /api/v1/overview", server.requireAuth(http.HandlerFunc(server.overview)))
	server.mux.Handle("GET /api/v1/stream", server.requireAuth(http.HandlerFunc(server.eventStream)))
	server.mux.Handle("GET /api/v1/connect/summary", server.requireAuth(http.HandlerFunc(server.connectSummary)))
	server.mux.Handle("GET /api/v1/connect/analytics", server.requireAuth(http.HandlerFunc(server.connectAnalytics)))
	server.mux.Handle("GET /api/v1/connect/setup", server.requireAuth(http.HandlerFunc(server.connectSetup)))
	server.mux.Handle("GET /api/v1/connect/llm/configuration", server.requireAuth(http.HandlerFunc(server.llmConfiguration)))
	server.mux.Handle("GET /api/v1/connect/llm/providers", server.requireAuth(http.HandlerFunc(server.providers)))
	server.mux.Handle("POST /api/v1/connect/llm/providers", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.createLLMProvider))))
	server.mux.Handle("GET /api/v1/connect/llm/providers/{resourceId}", server.requireAuth(http.HandlerFunc(server.provider)))
	server.mux.Handle("PATCH /api/v1/connect/llm/providers/{resourceId}", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.updateLLMProvider))))
	server.mux.Handle("DELETE /api/v1/connect/llm/providers/{resourceId}", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.deleteLLMProvider))))
	server.mux.Handle("GET /api/v1/connect/llm/models", server.requireAuth(http.HandlerFunc(server.models)))
	server.mux.Handle("POST /api/v1/connect/llm/models", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.createLLMModel))))
	server.mux.Handle("GET /api/v1/connect/llm/models/{resourceId}", server.requireAuth(http.HandlerFunc(server.gatewayModel)))
	server.mux.Handle("PATCH /api/v1/connect/llm/models/{resourceId}", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.updateLLMModel))))
	server.mux.Handle("DELETE /api/v1/connect/llm/models/{resourceId}", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.deleteLLMModel))))
	server.mux.Handle("GET /api/v1/connect/mcp/configuration", server.requireAuth(http.HandlerFunc(server.mcpConfiguration)))
	server.mux.Handle("PATCH /api/v1/connect/mcp/configuration/settings", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.updateMCPSettings))))
	server.mux.Handle("GET /api/v1/connect/mcp/servers", server.requireAuth(http.HandlerFunc(server.mcpServers)))
	server.mux.Handle("POST /api/v1/connect/mcp/servers", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.createMCPServer))))
	server.mux.Handle("GET /api/v1/connect/mcp/servers/{resourceId}", server.requireAuth(http.HandlerFunc(server.mcpServer)))
	server.mux.Handle("PATCH /api/v1/connect/mcp/servers/{resourceId}", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.updateMCPServer))))
	server.mux.Handle("DELETE /api/v1/connect/mcp/servers/{resourceId}", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.deleteMCPServer))))
	server.mux.Handle("GET /api/v1/connect/traffic/routes", server.requireAuth(http.HandlerFunc(server.trafficRoutes)))
	server.mux.Handle("POST /api/v1/connect/traffic/routes", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.createTrafficRoute))))
	server.mux.Handle("GET /api/v1/connect/traffic/routes/{resourceId}", server.requireAuth(http.HandlerFunc(server.route)))
	server.mux.Handle("PATCH /api/v1/connect/traffic/routes/{resourceId}", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.updateTrafficRoute))))
	server.mux.Handle("DELETE /api/v1/connect/traffic/routes/{resourceId}", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.deleteTrafficRoute))))
	server.mux.Handle("GET /api/v1/connect/traffic/configuration", server.requireAuth(http.HandlerFunc(server.trafficConfiguration)))
	server.mux.Handle("POST /api/v1/connect/traffic/binds", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.createTrafficBind))))
	server.mux.Handle("PATCH /api/v1/connect/traffic/binds/{resourceId}", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.updateTrafficBind))))
	server.mux.Handle("DELETE /api/v1/connect/traffic/binds/{resourceId}", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.deleteTrafficBind))))
	server.mux.Handle("POST /api/v1/connect/traffic/listeners", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.createTrafficListener))))
	server.mux.Handle("PATCH /api/v1/connect/traffic/listeners/{resourceId}", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.updateTrafficListener))))
	server.mux.Handle("DELETE /api/v1/connect/traffic/listeners/{resourceId}", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.deleteTrafficListener))))
	server.mux.Handle("GET /api/v1/trust/agents", server.requireAuth(http.HandlerFunc(server.trustAgents)))
	server.mux.Handle("GET /api/v1/trust/agents/{agentId}", server.requireAuth(http.HandlerFunc(server.trustAgent)))
	server.mux.Handle("GET /api/v1/trust/resources", server.requireAuth(http.HandlerFunc(server.trustResources)))
	server.mux.Handle("GET /api/v1/trust/scans", server.requireAuth(http.HandlerFunc(server.trustScans)))
	server.mux.Handle("GET /api/v1/trust/scans/{scanId}", server.requireAuth(http.HandlerFunc(server.trustScan)))
	server.mux.Handle("PATCH /api/v1/trust/agents/{agentId}/tools/{tool}/labels", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.updateToolLabels))))
	server.mux.Handle("POST /api/v1/trust/agents/{agentId}/skills/detect", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.detectSkills))))
	server.mux.Handle("POST /api/v1/trust/agents/{agentId}/mcps/detect", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.detectMCPs))))
	server.mux.Handle("GET /api/v1/protect/policies", server.requireAuth(http.HandlerFunc(server.protectPolicies)))
	server.mux.Handle("GET /api/v1/protect/gateway-policies/configuration", server.requireAuth(http.HandlerFunc(server.gatewayPolicyConfiguration)))
	server.mux.Handle("PATCH /api/v1/protect/gateway-policies/{resourceId}", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.upsertGatewayPolicy))))
	server.mux.Handle("DELETE /api/v1/protect/gateway-policies/{resourceId}", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.deleteGatewayPolicy))))
	server.mux.Handle("POST /api/v1/protect/runtime-rules/check", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.checkRuntimeRule))))
	server.mux.Handle("POST /api/v1/protect/agents/{agentId}/runtime-rules", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.publishRuntimeRule))))
	server.mux.Handle("DELETE /api/v1/protect/agents/{agentId}/runtime-rules/{ruleId}", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.deleteRuntimeRule))))
	server.mux.Handle("GET /api/v1/protect/approvals", server.requireAuth(http.HandlerFunc(server.protectApprovals)))
	server.mux.Handle("POST /api/v1/protect/approvals/{ticketId}/approve", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.approveTicket))))
	server.mux.Handle("POST /api/v1/protect/approvals/{ticketId}/deny", server.requireAuth(server.requireCSRF(http.HandlerFunc(server.denyTicket))))
	server.mux.Handle("GET /api/v1/audit/analytics", server.requireAuth(http.HandlerFunc(server.auditAnalytics)))
	server.mux.Handle("GET /api/v1/audit/events", server.requireAuth(http.HandlerFunc(server.auditEvents)))
	server.mux.Handle("GET /api/v1/audit/events/{source}/{eventId}", server.requireAuth(http.HandlerFunc(server.auditEvent)))
	server.mux.Handle("GET /api/v1/audit/sessions", server.requireAuth(http.HandlerFunc(server.auditSessions)))
	server.mux.Handle("/api/v1/", server.requireAuth(http.HandlerFunc(server.notImplemented)))
}

func (server *server) liveness(writer http.ResponseWriter, _ *http.Request) {
	server.writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *server) readiness(writer http.ResponseWriter, request *http.Request) {
	if server.config.Readiness != nil {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := server.config.Readiness.Ready(ctx); err != nil {
			server.writeError(writer, request, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "persistent storage is unavailable or migrations are incomplete", nil, true)
			return
		}
	}
	server.writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
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

func (server *server) sessionInfo(writer http.ResponseWriter, request *http.Request) {
	if !server.config.AuthEnabled {
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	session, ok := server.config.Sessions.Authenticate(request)
	if !ok {
		server.writeError(writer, request, http.StatusUnauthorized, "UNAUTHORIZED", "authentication is required", nil, false)
		return
	}
	writer.Header().Set("X-CSRF-Token", server.config.Sessions.CSRFToken(session))
	writer.WriteHeader(http.StatusNoContent)
}

func (server *server) health(writer http.ResponseWriter, request *http.Request) {
	server.writeJSON(writer, http.StatusOK, server.config.Aggregate.Health())
}

func (server *server) capabilities(writer http.ResponseWriter, request *http.Request) {
	server.writeJSON(writer, http.StatusOK, server.config.Aggregate.Capabilities(request.Context()))
}

func (server *server) diagnostics(writer http.ResponseWriter, _ *http.Request) {
	server.writeJSON(writer, http.StatusOK, server.config.Aggregate.Diagnostics())
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

func (server *server) llmConfiguration(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Connect.LLMConfiguration(request.Context())
	server.writeConnectResult(writer, request, envelope, err)
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

func (server *server) createLLMProvider(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	var input model.LLMProviderMutationRequest
	if !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.CreateProvider(request.Context(), input)
	server.writeConnectMutation(writer, request, http.StatusCreated, envelope, err)
}

func (server *server) updateLLMProvider(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	var input model.LLMProviderMutationRequest
	if !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.UpdateProvider(request.Context(), request.PathValue("resourceId"), input)
	server.writeConnectMutation(writer, request, http.StatusOK, envelope, err)
}

func (server *server) deleteLLMProvider(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	var input model.LLMProviderDeleteRequest
	if !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.DeleteProvider(request.Context(), request.PathValue("resourceId"), input)
	server.writeConnectMutation(writer, request, http.StatusOK, envelope, err)
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

func (server *server) createLLMModel(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	var input model.LLMModelMutationRequest
	if !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.CreateModel(request.Context(), input)
	server.writeConnectMutation(writer, request, http.StatusCreated, envelope, err)
}

func (server *server) updateLLMModel(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	var input model.LLMModelMutationRequest
	if !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.UpdateModel(request.Context(), request.PathValue("resourceId"), input)
	server.writeConnectMutation(writer, request, http.StatusOK, envelope, err)
}

func (server *server) deleteLLMModel(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	var input model.LLMDeleteRequest
	if !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.DeleteModel(request.Context(), request.PathValue("resourceId"), input)
	server.writeConnectMutation(writer, request, http.StatusOK, envelope, err)
}

func (server *server) mcpServers(writer http.ResponseWriter, request *http.Request) {
	query, ok := server.resourceQuery(writer, request)
	if !ok || !server.connectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Connect.MCPServers(request.Context(), query.search, query.cursor, query.limit)
	server.writeConnectResult(writer, request, envelope, err)
}

func (server *server) mcpConfiguration(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Connect.MCPConfiguration(request.Context())
	server.writeConnectResult(writer, request, envelope, err)
}

func (server *server) updateMCPSettings(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	var input model.MCPSettingsMutationRequest
	if !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.UpdateMCPSettings(request.Context(), input)
	server.writeConnectMCPMutation(writer, request, http.StatusOK, envelope, err)
}

func (server *server) createMCPServer(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	var input model.MCPServerMutationRequest
	if !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.CreateMCPServer(request.Context(), input)
	server.writeConnectMCPMutation(writer, request, http.StatusCreated, envelope, err)
}

func (server *server) mcpServer(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Connect.MCPServer(request.Context(), request.PathValue("resourceId"))
	server.writeConnectResult(writer, request, envelope, err)
}

func (server *server) updateMCPServer(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	var input model.MCPServerMutationRequest
	if !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.UpdateMCPServer(request.Context(), request.PathValue("resourceId"), input)
	server.writeConnectMCPMutation(writer, request, http.StatusOK, envelope, err)
}

func (server *server) deleteMCPServer(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	var input model.MCPDeleteRequest
	if !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.DeleteMCPServer(request.Context(), request.PathValue("resourceId"), input)
	server.writeConnectMCPMutation(writer, request, http.StatusOK, envelope, err)
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

func (server *server) trafficConfiguration(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Connect.TrafficConfiguration(request.Context())
	server.writeConnectResult(writer, request, envelope, err)
}

func (server *server) createTrafficBind(writer http.ResponseWriter, request *http.Request) {
	var input model.TrafficBindMutationRequest
	if !server.connectAvailable(writer, request) || !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.CreateTrafficBind(request.Context(), input)
	server.writeConnectTrafficMutation(writer, request, http.StatusCreated, envelope, err)
}

func (server *server) updateTrafficBind(writer http.ResponseWriter, request *http.Request) {
	var input model.TrafficBindMutationRequest
	if !server.connectAvailable(writer, request) || !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.UpdateTrafficBind(request.Context(), request.PathValue("resourceId"), input)
	server.writeConnectTrafficMutation(writer, request, http.StatusOK, envelope, err)
}

func (server *server) deleteTrafficBind(writer http.ResponseWriter, request *http.Request) {
	var input model.TrafficDeleteRequest
	if !server.connectAvailable(writer, request) || !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.DeleteTrafficBind(request.Context(), request.PathValue("resourceId"), input)
	server.writeConnectTrafficMutation(writer, request, http.StatusOK, envelope, err)
}

func (server *server) createTrafficListener(writer http.ResponseWriter, request *http.Request) {
	var input model.TrafficListenerMutationRequest
	if !server.connectAvailable(writer, request) || !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.CreateTrafficListener(request.Context(), input)
	server.writeConnectTrafficMutation(writer, request, http.StatusCreated, envelope, err)
}

func (server *server) updateTrafficListener(writer http.ResponseWriter, request *http.Request) {
	var input model.TrafficListenerMutationRequest
	if !server.connectAvailable(writer, request) || !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.UpdateTrafficListener(request.Context(), request.PathValue("resourceId"), input)
	server.writeConnectTrafficMutation(writer, request, http.StatusOK, envelope, err)
}

func (server *server) deleteTrafficListener(writer http.ResponseWriter, request *http.Request) {
	var input model.TrafficDeleteRequest
	if !server.connectAvailable(writer, request) || !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.DeleteTrafficListener(request.Context(), request.PathValue("resourceId"), input)
	server.writeConnectTrafficMutation(writer, request, http.StatusOK, envelope, err)
}

func (server *server) createTrafficRoute(writer http.ResponseWriter, request *http.Request) {
	var input model.TrafficRouteMutationRequest
	if !server.connectAvailable(writer, request) || !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.CreateTrafficRoute(request.Context(), input)
	server.writeConnectTrafficMutation(writer, request, http.StatusCreated, envelope, err)
}

func (server *server) updateTrafficRoute(writer http.ResponseWriter, request *http.Request) {
	var input model.TrafficRouteMutationRequest
	if !server.connectAvailable(writer, request) || !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.UpdateTrafficRoute(request.Context(), request.PathValue("resourceId"), input)
	server.writeConnectTrafficMutation(writer, request, http.StatusOK, envelope, err)
}

func (server *server) deleteTrafficRoute(writer http.ResponseWriter, request *http.Request) {
	var input model.TrafficDeleteRequest
	if !server.connectAvailable(writer, request) || !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.DeleteTrafficRoute(request.Context(), request.PathValue("resourceId"), input)
	server.writeConnectTrafficMutation(writer, request, http.StatusOK, envelope, err)
}

func (server *server) gatewayPolicyConfiguration(writer http.ResponseWriter, request *http.Request) {
	if !server.connectAvailable(writer, request) {
		return
	}
	envelope, err := server.config.Connect.GatewayPolicyConfiguration(request.Context())
	server.writeConnectResult(writer, request, envelope, err)
}

func (server *server) upsertGatewayPolicy(writer http.ResponseWriter, request *http.Request) {
	var input model.GatewayPolicyMutationRequest
	if !server.connectAvailable(writer, request) || !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.UpsertGatewayPolicy(request.Context(), request.PathValue("resourceId"), input)
	server.writeGatewayPolicyMutation(writer, request, envelope, err)
}

func (server *server) deleteGatewayPolicy(writer http.ResponseWriter, request *http.Request) {
	var input model.GatewayPolicyDeleteRequest
	if !server.connectAvailable(writer, request) || !server.decodeMutation(writer, request, &input) {
		return
	}
	envelope, err := server.config.Connect.DeleteGatewayPolicy(request.Context(), request.PathValue("resourceId"), input)
	server.writeGatewayPolicyMutation(writer, request, envelope, err)
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

func (server *server) auditAnalytics(writer http.ResponseWriter, request *http.Request) {
	if !server.auditAvailable(writer, request) {
		return
	}
	server.writeJSON(writer, http.StatusOK, server.config.Audit.Snapshot())
}

func (server *server) auditEvents(writer http.ResponseWriter, request *http.Request) {
	if !server.auditAvailable(writer, request) {
		return
	}
	query, ok := server.resourceQuery(writer, request)
	if !ok {
		return
	}
	sourceFilter := model.Source(strings.TrimSpace(request.URL.Query().Get("source")))
	if sourceFilter != "" && sourceFilter != model.SourceAgentGateway && sourceFilter != model.SourceAgentGuard {
		server.writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "audit source filter is invalid", nil, false)
		return
	}
	envelope, err := server.config.Audit.Events(request.Context(), sourceFilter, query.cursor, query.limit)
	if errors.Is(err, audit.ErrInvalidCursor) {
		server.writeError(writer, request, http.StatusBadRequest, "INVALID_CURSOR", "pagination cursor is invalid", nil, false)
		return
	}
	if errors.Is(err, audit.ErrStorageUnavailable) {
		server.writeError(writer, request, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "persistent Audit storage is unavailable", nil, true)
		return
	}
	server.writeJSON(writer, http.StatusOK, envelope)
}

func (server *server) auditEvent(writer http.ResponseWriter, request *http.Request) {
	if !server.auditAvailable(writer, request) {
		return
	}
	sourceValue := model.Source(request.PathValue("source"))
	if sourceValue != model.SourceAgentGateway && sourceValue != model.SourceAgentGuard {
		server.writeError(writer, request, http.StatusNotFound, "NOT_FOUND", "audit event was not found", nil, false)
		return
	}
	event, err := server.config.Audit.Detail(request.Context(), sourceValue, request.PathValue("eventId"))
	if errors.Is(err, audit.ErrEventNotFound) {
		server.writeError(writer, request, http.StatusNotFound, "NOT_FOUND", "audit event was not found", source(sourceValue), false)
		return
	}
	if errors.Is(err, audit.ErrDetailUnavailable) {
		server.writeError(writer, request, http.StatusServiceUnavailable, "DETAIL_UNAVAILABLE", "complete upstream audit detail is unavailable", source(sourceValue), true)
		return
	}
	if errors.Is(err, audit.ErrStorageUnavailable) {
		server.writeError(writer, request, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "persistent Audit storage is unavailable", nil, true)
		return
	}
	var gatewayContract *gateway.ContractError
	if errors.As(err, &gatewayContract) {
		server.writeError(writer, request, http.StatusBadGateway, "UPSTREAM_CONTRACT_MISMATCH", gatewayContract.Error(), source(sourceValue), false)
		return
	}
	var upstreamError *upstream.Error
	if errors.As(err, &upstreamError) {
		server.writeError(writer, request, http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE", "complete upstream audit detail is unavailable", source(sourceValue), upstreamError.Retryable)
		return
	}
	if err != nil {
		server.writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "the request could not be completed", source(sourceValue), true)
		return
	}
	snapshot := server.config.Audit.Snapshot()
	server.writeJSON(writer, http.StatusOK, model.ResourceEnvelope[model.UnifiedEvent]{Data: event, Meta: snapshot.Meta})
}

func (server *server) auditSessions(writer http.ResponseWriter, request *http.Request) {
	if !server.auditAvailable(writer, request) {
		return
	}
	snapshot := server.config.Audit.Snapshot()
	server.writeJSON(writer, http.StatusOK, model.ResourceEnvelope[[]model.AuditSession]{Data: snapshot.Data.Sessions, Meta: snapshot.Meta})
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

func (server *server) auditAvailable(writer http.ResponseWriter, request *http.Request) bool {
	if server.config.Audit == nil {
		server.writeError(writer, request, http.StatusServiceUnavailable, "AUDIT_UNAVAILABLE", "Audit integration is unavailable", nil, true)
		return false
	}
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	if err := server.config.Audit.Ready(ctx); err != nil {
		server.writeError(writer, request, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "persistent Audit storage is unavailable", nil, true)
		return false
	}
	return true
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
	if errors.Is(err, connect.ErrInvalidRequest) {
		server.writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "the agentgateway configuration request is invalid", source(model.SourceAgentGateway), false)
		return
	}
	if errors.Is(err, connect.ErrRevisionStale) {
		server.writeError(writer, request, http.StatusConflict, "CONFIGURATION_CHANGED", "agentgateway configuration changed; refresh before saving again", source(model.SourceAgentGateway), false)
		return
	}
	if errors.Is(err, connect.ErrReferenced) {
		server.writeError(writer, request, http.StatusConflict, "RESOURCE_REFERENCED", "the agentgateway configuration resource still has references or incompatible children", source(model.SourceAgentGateway), false)
		return
	}
	if errors.Is(err, connect.ErrConflict) {
		server.writeError(writer, request, http.StatusConflict, "RESOURCE_CONFLICT", "an agentgateway configuration resource already uses that value", source(model.SourceAgentGateway), false)
		return
	}
	if errors.Is(err, connect.ErrMutationInFlight) {
		server.writeError(writer, request, http.StatusConflict, "MUTATION_IN_FLIGHT", "another agentgateway configuration change is still in progress", source(model.SourceAgentGateway), false)
		return
	}
	if errors.Is(err, gateway.ErrLLMWriteUnverified) || errors.Is(err, gateway.ErrMCPWriteUnverified) || errors.Is(err, gateway.ErrTrafficWriteUnverified) || errors.Is(err, gateway.ErrGatewayPolicyWriteUnverified) {
		server.writeError(writer, request, http.StatusServiceUnavailable, "WRITE_UNVERIFIED", "the write result could not be verified; refresh before making another change", source(model.SourceAgentGateway), false)
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

func (server *server) writeConnectMutation(writer http.ResponseWriter, request *http.Request, status int, envelope model.LLMMutationEnvelope, err error) {
	if err != nil {
		server.writeConnectResult(writer, request, nil, err)
		return
	}
	envelope.Data.RequestID = requestID(request.Context())
	server.config.Logger.Info("connect operation completed",
		"request_id", envelope.Data.RequestID,
		"operation", envelope.Data.Operation,
		"target", envelope.Data.Target,
		"status", envelope.Data.Status,
	)
	server.writeJSON(writer, status, envelope)
}

func (server *server) writeConnectMCPMutation(writer http.ResponseWriter, request *http.Request, status int, envelope model.MCPMutationEnvelope, err error) {
	if err != nil {
		server.writeConnectResult(writer, request, nil, err)
		return
	}
	envelope.Data.RequestID = requestID(request.Context())
	server.config.Logger.Info("connect operation completed",
		"request_id", envelope.Data.RequestID,
		"operation", envelope.Data.Operation,
		"target", envelope.Data.Target,
		"status", envelope.Data.Status,
	)
	server.writeJSON(writer, status, envelope)
}

func (server *server) writeConnectTrafficMutation(writer http.ResponseWriter, request *http.Request, status int, envelope model.TrafficMutationEnvelope, err error) {
	if err != nil {
		server.writeConnectResult(writer, request, nil, err)
		return
	}
	envelope.Data.RequestID = requestID(request.Context())
	server.config.Logger.Info("connect operation completed",
		"request_id", envelope.Data.RequestID,
		"operation", envelope.Data.Operation,
		"target", envelope.Data.Target,
		"status", envelope.Data.Status,
	)
	server.writeJSON(writer, status, envelope)
}

func (server *server) writeGatewayPolicyMutation(writer http.ResponseWriter, request *http.Request, envelope model.GatewayPolicyMutationEnvelope, err error) {
	if err != nil {
		server.writeConnectResult(writer, request, nil, err)
		return
	}
	envelope.Data.RequestID = requestID(request.Context())
	server.config.Logger.Info("protect gateway policy operation completed",
		"request_id", envelope.Data.RequestID,
		"operation", envelope.Data.Operation,
		"target", envelope.Data.Target,
		"status", envelope.Data.Status,
	)
	server.writeJSON(writer, http.StatusOK, envelope)
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
	if errors.Is(err, protect.ErrAuditPersistence) {
		server.writeError(writer, request, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "persistent Audit storage is unavailable; inspect the approval state before retrying", nil, false)
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
	if server.config.Outbox == nil {
		server.writeError(writer, request, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "persistent event streaming is unavailable", nil, true)
		return
	}
	if server.config.Readiness != nil {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := server.config.Readiness.Ready(ctx); err != nil {
			server.writeError(writer, request, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "persistent event streaming is unavailable", nil, true)
			return
		}
	}
	flusher, ok := writer.(http.Flusher)
	if !ok {
		server.writeError(writer, request, http.StatusInternalServerError, "STREAM_UNAVAILABLE", "event streaming is unavailable", nil, true)
		return
	}
	rawLastSequence, hasLastSequence := request.Header["Last-Event-Id"]
	lastSequence := int64(0)
	if hasLastSequence {
		raw := ""
		if len(rawLastSequence) > 0 {
			raw = strings.TrimSpace(rawLastSequence[0])
		}
		parsed, err := parseLastEventID(raw)
		if err != nil {
			server.writeError(writer, request, http.StatusBadRequest, "INVALID_LAST_EVENT_ID", "Last-Event-ID must be a non-negative 64-bit integer", nil, false)
			return
		}
		lastSequence = parsed
	}
	notifications, unsubscribe := server.config.Stream.Subscribe()
	defer unsubscribe()
	const batchSize = 1000
	batch, err := server.config.Outbox.ReplayAfter(request.Context(), lastSequence, batchSize)
	if err != nil {
		server.writeError(writer, request, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "persistent event streaming is unavailable", nil, true)
		return
	}
	resetReason := ""
	if hasLastSequence {
		if lastSequence > batch.Latest {
			resetReason = "cursor_ahead"
		} else if (batch.Oldest > 0 && lastSequence < batch.Oldest) ||
			(batch.Oldest == 0 && batch.Latest > lastSequence) {
			resetReason = "outbox_retention"
		}
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache, no-store")
	writer.Header().Set("Connection", "keep-alive")
	writer.Header().Set("X-Accel-Buffering", "no")
	if resetReason != "" {
		if err := writeSSEReset(writer, batch.Latest, resetReason, batch.Oldest); err != nil {
			return
		}
		lastSequence = batch.Latest
	} else {
		for _, message := range batch.Messages {
			if err := writeSSE(writer, message.Sequence, message.Event); err != nil {
				return
			}
			lastSequence = message.Sequence
		}
	}
	flusher.Flush()

	drain := func() error {
		for {
			batch, err := server.config.Outbox.ReplayAfter(request.Context(), lastSequence, batchSize)
			if err != nil {
				return err
			}
			for _, message := range batch.Messages {
				if message.Sequence <= lastSequence {
					continue
				}
				if err := writeSSE(writer, message.Sequence, message.Event); err != nil {
					return err
				}
				lastSequence = message.Sequence
			}
			if len(batch.Messages) < batchSize {
				return nil
			}
		}
	}
	if resetReason == "" && len(batch.Messages) == batchSize {
		if err := drain(); err != nil {
			return
		}
		flusher.Flush()
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case _, open := <-notifications:
			if !open || drain() != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if drain() != nil {
				return
			}
			if _, err := fmt.Fprint(writer, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func parseLastEventID(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || (len(raw) > 1 && raw[0] == '0') {
		return 0, errors.New("invalid Last-Event-ID")
	}
	for _, character := range raw {
		if character < '0' || character > '9' {
			return 0, errors.New("invalid Last-Event-ID")
		}
	}
	return strconv.ParseInt(raw, 10, 64)
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

func writeSSE(writer http.ResponseWriter, sequence int64, event model.UnifiedEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "id: %d\nevent: %s\ndata: %s\n\n", sequence, event.Kind, payload)
	return err
}

func writeSSEReset(writer http.ResponseWriter, sequence int64, reason string, oldest int64) error {
	payload, err := json.Marshal(map[string]any{
		"reason": reason, "resumeAfter": sequence, "oldestAvailable": oldest,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "id: %d\nevent: reset\ndata: %s\n\n", sequence, payload)
	return err
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
