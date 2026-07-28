// Package connect implements the agentgateway management BFF.
package connect

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/gateway"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

const (
	defaultLimit = 25
	maxLimit     = 100
	maxRevisions = 100
	revisionTTL  = 5 * time.Minute
)

var (
	ErrInvalidCursor    = errors.New("invalid pagination cursor")
	ErrNotFound         = errors.New("resource not found")
	ErrInvalidRequest   = errors.New("invalid connect request")
	ErrRevisionStale    = errors.New("configuration revision is stale")
	ErrConflict         = errors.New("configuration resource conflicts with current state")
	ErrReferenced       = errors.New("configuration resource is still referenced")
	ErrMutationInFlight = errors.New("agentgateway configuration mutation already in progress")
)

type Gateway interface {
	Health(context.Context) model.SourceHealth
	Snapshot(context.Context) (model.GatewaySnapshot, error)
	Analytics(context.Context) (model.GatewayAnalytics, error)
	LLMConfiguration(context.Context) (model.LLMConfiguration, string, error)
	ApplyLLMChange(context.Context, string, model.LLMChange) (string, error)
	MCPConfiguration(context.Context) (model.MCPConfiguration, string, error)
	ApplyMCPChange(context.Context, string, model.MCPChange) (string, error)
	TrafficConfiguration(context.Context) (model.TrafficConfiguration, string, error)
	ApplyTrafficChange(context.Context, string, model.TrafficChange) (string, error)
	PolicyConfiguration(context.Context) (model.GatewayPolicyConfiguration, string, error)
	ApplyPolicyChange(context.Context, string, model.GatewayPolicyChange) (string, error)
}

type revisionState struct {
	revision  string
	createdAt time.Time
	expiresAt time.Time
}

type Service struct {
	gateway   Gateway
	links     model.ConsoleLinks
	mu        sync.Mutex
	revisions map[string]revisionState
	mutating  bool
}

func New(gateway Gateway, consoleURL string) *Service {
	return &Service{
		gateway: gateway, links: consoleLinks(consoleURL), revisions: make(map[string]revisionState),
	}
}

func (service *Service) Links() model.ConsoleLinks { return service.links }

func (service *Service) LLMConfiguration(ctx context.Context) (model.LLMConfigurationEnvelope, error) {
	configuration, revision, err := service.gateway.LLMConfiguration(ctx)
	if err != nil {
		return model.LLMConfigurationEnvelope{}, err
	}
	token, err := service.issueRevision(revision)
	if err != nil {
		return model.LLMConfigurationEnvelope{}, err
	}
	configuration.RevisionToken = token
	configuration.Links = service.links
	return model.LLMConfigurationEnvelope{
		Data: configuration,
		Meta: gatewayMeta(configuration.FetchedAt, false),
	}, nil
}

func (service *Service) MCPConfiguration(ctx context.Context) (model.MCPConfigurationEnvelope, error) {
	configuration, revision, err := service.gateway.MCPConfiguration(ctx)
	if err != nil {
		return model.MCPConfigurationEnvelope{}, err
	}
	token, err := service.issueRevision(revision)
	if err != nil {
		return model.MCPConfigurationEnvelope{}, err
	}
	configuration.RevisionToken = token
	configuration.Links = service.links
	return model.MCPConfigurationEnvelope{
		Data: configuration,
		Meta: gatewayMeta(configuration.FetchedAt, false),
	}, nil
}

func (service *Service) TrafficConfiguration(ctx context.Context) (model.TrafficConfigurationEnvelope, error) {
	configuration, revision, err := service.gateway.TrafficConfiguration(ctx)
	if err != nil {
		return model.TrafficConfigurationEnvelope{}, err
	}
	token, err := service.issueRevision(revision)
	if err != nil {
		return model.TrafficConfigurationEnvelope{}, err
	}
	configuration.RevisionToken = token
	configuration.Links = service.links
	return model.TrafficConfigurationEnvelope{
		Data: configuration,
		Meta: gatewayMeta(configuration.FetchedAt, false),
	}, nil
}

func (service *Service) GatewayPolicyConfiguration(ctx context.Context) (model.GatewayPolicyConfigurationEnvelope, error) {
	configuration, revision, err := service.gateway.PolicyConfiguration(ctx)
	if err != nil {
		return model.GatewayPolicyConfigurationEnvelope{}, err
	}
	token, err := service.issueRevision(revision)
	if err != nil {
		return model.GatewayPolicyConfigurationEnvelope{}, err
	}
	configuration.RevisionToken = token
	configuration.Links = service.links
	return model.GatewayPolicyConfigurationEnvelope{
		Data: configuration,
		Meta: gatewayMeta(configuration.FetchedAt, false),
	}, nil
}

func (service *Service) CreateProvider(ctx context.Context, request model.LLMProviderMutationRequest) (model.LLMMutationEnvelope, error) {
	return service.applyLLMChange(ctx, request.RevisionToken, model.LLMChange{
		Operation: "create-llm-provider", Provider: request.Provider,
	}, request.Provider.Name, "LLM provider created")
}

func (service *Service) UpdateProvider(ctx context.Context, id string, request model.LLMProviderMutationRequest) (model.LLMMutationEnvelope, error) {
	return service.applyLLMChange(ctx, request.RevisionToken, model.LLMChange{
		Operation: "update-llm-provider", ResourceID: id, Provider: request.Provider,
	}, request.Provider.Name, "LLM provider updated")
}

func (service *Service) DeleteProvider(ctx context.Context, id string, request model.LLMProviderDeleteRequest) (model.LLMMutationEnvelope, error) {
	if !request.Confirmed || request.DeleteReferencedModels == nil {
		return model.LLMMutationEnvelope{}, ErrInvalidRequest
	}
	return service.applyLLMChange(ctx, request.RevisionToken, model.LLMChange{
		Operation: "delete-llm-provider", ResourceID: id,
		DeleteReferencedModels: *request.DeleteReferencedModels,
	}, id, "LLM provider deleted")
}

func (service *Service) CreateModel(ctx context.Context, request model.LLMModelMutationRequest) (model.LLMMutationEnvelope, error) {
	return service.applyLLMChange(ctx, request.RevisionToken, model.LLMChange{
		Operation: "create-llm-model", Model: request.Model,
	}, request.Model.Name, "LLM model created")
}

func (service *Service) UpdateModel(ctx context.Context, id string, request model.LLMModelMutationRequest) (model.LLMMutationEnvelope, error) {
	return service.applyLLMChange(ctx, request.RevisionToken, model.LLMChange{
		Operation: "update-llm-model", ResourceID: id, Model: request.Model,
	}, request.Model.Name, "LLM model updated")
}

func (service *Service) DeleteModel(ctx context.Context, id string, request model.LLMDeleteRequest) (model.LLMMutationEnvelope, error) {
	if !request.Confirmed {
		return model.LLMMutationEnvelope{}, ErrInvalidRequest
	}
	return service.applyLLMChange(ctx, request.RevisionToken, model.LLMChange{
		Operation: "delete-llm-model", ResourceID: id,
	}, id, "LLM model deleted")
}

func (service *Service) UpdateMCPSettings(ctx context.Context, request model.MCPSettingsMutationRequest) (model.MCPMutationEnvelope, error) {
	return service.applyMCPChange(ctx, request.RevisionToken, model.MCPChange{
		Operation: "update-mcp-settings", Settings: request.Settings,
	}, "MCP settings", "MCP settings updated")
}

func (service *Service) CreateMCPServer(ctx context.Context, request model.MCPServerMutationRequest) (model.MCPMutationEnvelope, error) {
	return service.applyMCPChange(ctx, request.RevisionToken, model.MCPChange{
		Operation: "create-mcp-server", Server: request.Server,
	}, request.Server.Name, "MCP server created")
}

func (service *Service) UpdateMCPServer(ctx context.Context, id string, request model.MCPServerMutationRequest) (model.MCPMutationEnvelope, error) {
	return service.applyMCPChange(ctx, request.RevisionToken, model.MCPChange{
		Operation: "update-mcp-server", ResourceID: id, Server: request.Server,
	}, request.Server.Name, "MCP server updated")
}

func (service *Service) DeleteMCPServer(ctx context.Context, id string, request model.MCPDeleteRequest) (model.MCPMutationEnvelope, error) {
	if !request.Confirmed {
		return model.MCPMutationEnvelope{}, ErrInvalidRequest
	}
	return service.applyMCPChange(ctx, request.RevisionToken, model.MCPChange{
		Operation: "delete-mcp-server", ResourceID: id,
	}, id, "MCP server deleted")
}

func (service *Service) CreateTrafficBind(ctx context.Context, request model.TrafficBindMutationRequest) (model.TrafficMutationEnvelope, error) {
	return service.applyTrafficChange(ctx, request.RevisionToken, model.TrafficChange{
		Operation: "create-traffic-bind", Bind: request.Bind,
	}, fmt.Sprintf("Port %d", request.Bind.Port), "Traffic bind created")
}

func (service *Service) UpdateTrafficBind(ctx context.Context, id string, request model.TrafficBindMutationRequest) (model.TrafficMutationEnvelope, error) {
	return service.applyTrafficChange(ctx, request.RevisionToken, model.TrafficChange{
		Operation: "update-traffic-bind", ResourceID: id, Bind: request.Bind,
	}, fmt.Sprintf("Port %d", request.Bind.Port), "Traffic bind updated")
}

func (service *Service) DeleteTrafficBind(ctx context.Context, id string, request model.TrafficDeleteRequest) (model.TrafficMutationEnvelope, error) {
	if !request.Confirmed {
		return model.TrafficMutationEnvelope{}, ErrInvalidRequest
	}
	return service.applyTrafficChange(ctx, request.RevisionToken, model.TrafficChange{
		Operation: "delete-traffic-bind", ResourceID: id, DeleteChildren: request.DeleteChildren,
	}, id, "Traffic bind deleted")
}

func (service *Service) CreateTrafficListener(ctx context.Context, request model.TrafficListenerMutationRequest) (model.TrafficMutationEnvelope, error) {
	return service.applyTrafficChange(ctx, request.RevisionToken, model.TrafficChange{
		Operation: "create-traffic-listener", BindID: request.BindID, Listener: request.Listener,
	}, "Listener", "Traffic listener created")
}

func (service *Service) UpdateTrafficListener(ctx context.Context, id string, request model.TrafficListenerMutationRequest) (model.TrafficMutationEnvelope, error) {
	return service.applyTrafficChange(ctx, request.RevisionToken, model.TrafficChange{
		Operation: "update-traffic-listener", ResourceID: id, Listener: request.Listener,
	}, "Listener", "Traffic listener updated")
}

func (service *Service) DeleteTrafficListener(ctx context.Context, id string, request model.TrafficDeleteRequest) (model.TrafficMutationEnvelope, error) {
	if !request.Confirmed {
		return model.TrafficMutationEnvelope{}, ErrInvalidRequest
	}
	return service.applyTrafficChange(ctx, request.RevisionToken, model.TrafficChange{
		Operation: "delete-traffic-listener", ResourceID: id, DeleteChildren: request.DeleteChildren,
	}, id, "Traffic listener deleted")
}

func (service *Service) CreateTrafficRoute(ctx context.Context, request model.TrafficRouteMutationRequest) (model.TrafficMutationEnvelope, error) {
	return service.applyTrafficChange(ctx, request.RevisionToken, model.TrafficChange{
		Operation: "create-traffic-route", ListenerID: request.ListenerID, Route: request.Route,
	}, "Route", "Traffic route created")
}

func (service *Service) UpdateTrafficRoute(ctx context.Context, id string, request model.TrafficRouteMutationRequest) (model.TrafficMutationEnvelope, error) {
	return service.applyTrafficChange(ctx, request.RevisionToken, model.TrafficChange{
		Operation: "update-traffic-route", ResourceID: id, Route: request.Route,
	}, "Route", "Traffic route updated")
}

func (service *Service) DeleteTrafficRoute(ctx context.Context, id string, request model.TrafficDeleteRequest) (model.TrafficMutationEnvelope, error) {
	if !request.Confirmed {
		return model.TrafficMutationEnvelope{}, ErrInvalidRequest
	}
	return service.applyTrafficChange(ctx, request.RevisionToken, model.TrafficChange{
		Operation: "delete-traffic-route", ResourceID: id,
	}, id, "Traffic route deleted")
}

func (service *Service) UpsertGatewayPolicy(ctx context.Context, id string, request model.GatewayPolicyMutationRequest) (model.GatewayPolicyMutationEnvelope, error) {
	if request.Value == nil {
		return model.GatewayPolicyMutationEnvelope{}, ErrInvalidRequest
	}
	return service.applyGatewayPolicyChange(ctx, request.RevisionToken, model.GatewayPolicyChange{
		Operation: "upsert-gateway-policy", ResourceID: id, Value: request.Value,
	}, "Gateway policy saved")
}

func (service *Service) DeleteGatewayPolicy(ctx context.Context, id string, request model.GatewayPolicyDeleteRequest) (model.GatewayPolicyMutationEnvelope, error) {
	if !request.Confirmed {
		return model.GatewayPolicyMutationEnvelope{}, ErrInvalidRequest
	}
	return service.applyGatewayPolicyChange(ctx, request.RevisionToken, model.GatewayPolicyChange{
		Operation: "delete-gateway-policy", ResourceID: id,
	}, "Gateway policy deleted")
}

func (service *Service) applyGatewayPolicyChange(ctx context.Context, token string, change model.GatewayPolicyChange, message string) (model.GatewayPolicyMutationEnvelope, error) {
	if token == "" || len(token) > 128 || change.ResourceID == "" || len(change.ResourceID) > 512 {
		return model.GatewayPolicyMutationEnvelope{}, ErrInvalidRequest
	}
	revision, ok := service.consumeRevision(token)
	if !ok {
		return model.GatewayPolicyMutationEnvelope{}, ErrRevisionStale
	}
	if !service.beginMutation() {
		return model.GatewayPolicyMutationEnvelope{}, ErrMutationInFlight
	}
	defer service.endMutation()
	target, err := service.gateway.ApplyPolicyChange(ctx, revision, change)
	if err != nil {
		return model.GatewayPolicyMutationEnvelope{}, translateGatewayPolicyError(err)
	}
	if target == "" {
		target = change.ResourceID
	}
	completedAt := time.Now().UTC()
	return model.GatewayPolicyMutationEnvelope{
		Data: model.GatewayPolicyMutationReceipt{
			Operation: change.Operation, Status: "succeeded", Source: model.SourceAgentGateway,
			Target: target, CompletedAt: completedAt, Message: message,
		},
		Meta: gatewayMeta(completedAt, false),
	}, nil
}

func translateGatewayPolicyError(err error) error {
	switch {
	case errors.Is(err, gateway.ErrConfigurationChanged):
		return ErrRevisionStale
	case errors.Is(err, gateway.ErrGatewayPolicyInvalidRequest):
		return ErrInvalidRequest
	case errors.Is(err, gateway.ErrGatewayPolicyNotFound):
		return ErrNotFound
	default:
		return err
	}
}

func (service *Service) applyLLMChange(ctx context.Context, token string, change model.LLMChange, target, message string) (model.LLMMutationEnvelope, error) {
	if token == "" || len(token) > 128 || target == "" {
		return model.LLMMutationEnvelope{}, ErrInvalidRequest
	}
	revision, ok := service.consumeRevision(token)
	if !ok {
		return model.LLMMutationEnvelope{}, ErrRevisionStale
	}
	if !service.beginMutation() {
		return model.LLMMutationEnvelope{}, ErrMutationInFlight
	}
	defer service.endMutation()
	resolvedTarget, err := service.gateway.ApplyLLMChange(ctx, revision, change)
	if err != nil {
		return model.LLMMutationEnvelope{}, translateLLMError(err)
	}
	if resolvedTarget != "" {
		target = resolvedTarget
	}
	completedAt := time.Now().UTC()
	return model.LLMMutationEnvelope{
		Data: model.LLMMutationReceipt{
			Operation: change.Operation, Status: "succeeded", Source: model.SourceAgentGateway,
			Target: target, CompletedAt: completedAt, Message: message,
		},
		Meta: gatewayMeta(completedAt, false),
	}, nil
}

func translateLLMError(err error) error {
	switch {
	case errors.Is(err, gateway.ErrConfigurationChanged):
		return ErrRevisionStale
	case errors.Is(err, gateway.ErrLLMInvalidRequest):
		return ErrInvalidRequest
	case errors.Is(err, gateway.ErrLLMResourceNotFound):
		return ErrNotFound
	case errors.Is(err, gateway.ErrLLMResourceReferenced):
		return ErrReferenced
	case errors.Is(err, gateway.ErrLLMResourceConflict):
		return ErrConflict
	default:
		return err
	}
}

func (service *Service) applyMCPChange(ctx context.Context, token string, change model.MCPChange, target, message string) (model.MCPMutationEnvelope, error) {
	if token == "" || len(token) > 128 || target == "" {
		return model.MCPMutationEnvelope{}, ErrInvalidRequest
	}
	revision, ok := service.consumeRevision(token)
	if !ok {
		return model.MCPMutationEnvelope{}, ErrRevisionStale
	}
	if !service.beginMutation() {
		return model.MCPMutationEnvelope{}, ErrMutationInFlight
	}
	defer service.endMutation()
	resolvedTarget, err := service.gateway.ApplyMCPChange(ctx, revision, change)
	if err != nil {
		return model.MCPMutationEnvelope{}, translateMCPError(err)
	}
	if resolvedTarget != "" {
		target = resolvedTarget
	}
	completedAt := time.Now().UTC()
	return model.MCPMutationEnvelope{
		Data: model.MCPMutationReceipt{
			Operation: change.Operation, Status: "succeeded", Source: model.SourceAgentGateway,
			Target: target, CompletedAt: completedAt, Message: message,
		},
		Meta: gatewayMeta(completedAt, false),
	}, nil
}

func translateMCPError(err error) error {
	switch {
	case errors.Is(err, gateway.ErrConfigurationChanged):
		return ErrRevisionStale
	case errors.Is(err, gateway.ErrMCPInvalidRequest):
		return ErrInvalidRequest
	case errors.Is(err, gateway.ErrMCPResourceNotFound):
		return ErrNotFound
	case errors.Is(err, gateway.ErrMCPResourceConflict):
		return ErrConflict
	default:
		return err
	}
}

func (service *Service) applyTrafficChange(ctx context.Context, token string, change model.TrafficChange, target, message string) (model.TrafficMutationEnvelope, error) {
	if token == "" || len(token) > 128 || target == "" {
		return model.TrafficMutationEnvelope{}, ErrInvalidRequest
	}
	revision, ok := service.consumeRevision(token)
	if !ok {
		return model.TrafficMutationEnvelope{}, ErrRevisionStale
	}
	if !service.beginMutation() {
		return model.TrafficMutationEnvelope{}, ErrMutationInFlight
	}
	defer service.endMutation()
	resolvedTarget, err := service.gateway.ApplyTrafficChange(ctx, revision, change)
	if err != nil {
		return model.TrafficMutationEnvelope{}, translateTrafficError(err)
	}
	if resolvedTarget != "" {
		target = resolvedTarget
	}
	completedAt := time.Now().UTC()
	return model.TrafficMutationEnvelope{
		Data: model.TrafficMutationReceipt{
			Operation: change.Operation, Status: "succeeded", Source: model.SourceAgentGateway,
			Target: target, CompletedAt: completedAt, Message: message,
		},
		Meta: gatewayMeta(completedAt, false),
	}, nil
}

func translateTrafficError(err error) error {
	switch {
	case errors.Is(err, gateway.ErrConfigurationChanged):
		return ErrRevisionStale
	case errors.Is(err, gateway.ErrTrafficInvalidRequest):
		return ErrInvalidRequest
	case errors.Is(err, gateway.ErrTrafficResourceNotFound):
		return ErrNotFound
	case errors.Is(err, gateway.ErrTrafficResourceReferenced):
		return ErrReferenced
	case errors.Is(err, gateway.ErrTrafficResourceConflict):
		return ErrConflict
	default:
		return err
	}
}

func (service *Service) issueRevision(revision string) (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	now := time.Now().UTC()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.pruneRevisionsLocked(now)
	if len(service.revisions) >= maxRevisions {
		var oldestToken string
		var oldestTime time.Time
		for candidate, state := range service.revisions {
			if oldestToken == "" || state.createdAt.Before(oldestTime) {
				oldestToken, oldestTime = candidate, state.createdAt
			}
		}
		delete(service.revisions, oldestToken)
	}
	service.revisions[token] = revisionState{revision: revision, createdAt: now, expiresAt: now.Add(revisionTTL)}
	return token, nil
}

func (service *Service) consumeRevision(token string) (string, bool) {
	now := time.Now().UTC()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.pruneRevisionsLocked(now)
	state, ok := service.revisions[token]
	if !ok || !state.expiresAt.After(now) {
		return "", false
	}
	delete(service.revisions, token)
	return state.revision, true
}

func (service *Service) pruneRevisionsLocked(now time.Time) {
	for token, state := range service.revisions {
		if !state.expiresAt.After(now) {
			delete(service.revisions, token)
		}
	}
}

func (service *Service) beginMutation() bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.mutating {
		return false
	}
	service.mutating = true
	return true
}

func (service *Service) endMutation() {
	service.mu.Lock()
	service.mutating = false
	service.mu.Unlock()
}

func (service *Service) Summary(ctx context.Context) (model.ConnectSummaryEnvelope, error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ConnectSummaryEnvelope{}, err
	}
	analytics, err := service.gateway.Analytics(ctx)
	if err != nil {
		return model.ConnectSummaryEnvelope{}, err
	}
	health := service.gateway.Health(ctx)
	counts := []model.ConnectCount{
		count("listeners", "Listeners", snapshot.Listeners),
		count("routes", "Routes", len(snapshot.Routes)),
		count("backends", "Backends", snapshot.Backends),
		count("mcp-targets", "MCP targets", len(snapshot.MCP)),
	}
	return model.ConnectSummaryEnvelope{
		Data: model.ConnectSummary{Health: health, Counts: counts, Analytics: analytics, Links: service.links},
		Meta: gatewayMeta(snapshot.FetchedAt, analytics.Status == "unavailable"),
	}, nil
}

func (service *Service) Providers(ctx context.Context, query, cursor string, limit int) (model.ResourcePageEnvelope[model.GatewayProvider], error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ResourcePageEnvelope[model.GatewayProvider]{}, err
	}
	items := filter(snapshot.Providers, query, func(item model.GatewayProvider) string {
		return item.Name + " " + item.Kind
	})
	page, err := paginate(items, cursor, limit)
	return model.ResourcePageEnvelope[model.GatewayProvider]{Data: page, Meta: gatewayMeta(snapshot.FetchedAt, false)}, err
}

func (service *Service) Models(ctx context.Context, query, cursor string, limit int) (model.ResourcePageEnvelope[model.GatewayModel], error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ResourcePageEnvelope[model.GatewayModel]{}, err
	}
	items := filter(snapshot.Models, query, func(item model.GatewayModel) string {
		return item.Name + " " + item.Provider + " " + item.Routing + " " + strings.Join(item.Targets, " ")
	})
	page, err := paginate(items, cursor, limit)
	return model.ResourcePageEnvelope[model.GatewayModel]{Data: page, Meta: gatewayMeta(snapshot.FetchedAt, false)}, err
}

func (service *Service) MCPServers(ctx context.Context, query, cursor string, limit int) (model.ResourcePageEnvelope[model.GatewayMCPServer], error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ResourcePageEnvelope[model.GatewayMCPServer]{}, err
	}
	items := filter(snapshot.MCP, query, func(item model.GatewayMCPServer) string {
		return item.Name + " " + item.Transport + " " + item.Scope
	})
	page, err := paginate(items, cursor, limit)
	return model.ResourcePageEnvelope[model.GatewayMCPServer]{Data: page, Meta: gatewayMeta(snapshot.FetchedAt, false)}, err
}

func (service *Service) Routes(ctx context.Context, query, cursor string, limit int) (model.ResourcePageEnvelope[model.GatewayRoute], error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ResourcePageEnvelope[model.GatewayRoute]{}, err
	}
	items := filter(snapshot.Routes, query, func(item model.GatewayRoute) string {
		return item.Name + " " + item.Listener + " " + item.Protocol + " " + strings.Join(item.Hostnames, " ") + " " + strings.Join(item.Targets, " ")
	})
	page, err := paginate(items, cursor, limit)
	return model.ResourcePageEnvelope[model.GatewayRoute]{Data: page, Meta: gatewayMeta(snapshot.FetchedAt, false)}, err
}

func (service *Service) Provider(ctx context.Context, id string) (model.ResourceEnvelope[model.GatewayProvider], error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ResourceEnvelope[model.GatewayProvider]{}, err
	}
	return detail(snapshot.Providers, id, snapshot.FetchedAt, func(item model.GatewayProvider) string { return item.ID })
}

func (service *Service) Model(ctx context.Context, id string) (model.ResourceEnvelope[model.GatewayModel], error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ResourceEnvelope[model.GatewayModel]{}, err
	}
	return detail(snapshot.Models, id, snapshot.FetchedAt, func(item model.GatewayModel) string { return item.ID })
}

func (service *Service) MCPServer(ctx context.Context, id string) (model.ResourceEnvelope[model.GatewayMCPServer], error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ResourceEnvelope[model.GatewayMCPServer]{}, err
	}
	return detail(snapshot.MCP, id, snapshot.FetchedAt, func(item model.GatewayMCPServer) string { return item.ID })
}

func (service *Service) Route(ctx context.Context, id string) (model.ResourceEnvelope[model.GatewayRoute], error) {
	snapshot, err := service.gateway.Snapshot(ctx)
	if err != nil {
		return model.ResourceEnvelope[model.GatewayRoute]{}, err
	}
	return detail(snapshot.Routes, id, snapshot.FetchedAt, func(item model.GatewayRoute) string { return item.ID })
}

func (service *Service) Analytics(ctx context.Context) (model.ResourceEnvelope[model.GatewayAnalytics], error) {
	analytics, err := service.gateway.Analytics(ctx)
	if err != nil {
		return model.ResourceEnvelope[model.GatewayAnalytics]{}, err
	}
	return model.ResourceEnvelope[model.GatewayAnalytics]{
		Data: analytics, Meta: gatewayMeta(service.gateway.Health(ctx).CheckedAt, analytics.Status == "unavailable"),
	}, nil
}

func (service *Service) Setup(ctx context.Context) model.ResourceEnvelope[model.ConnectSetup] {
	health := service.gateway.Health(ctx)
	snapshot, err := service.gateway.Snapshot(ctx)
	readable := err == nil
	status := health.Status
	message := health.Message
	if !readable {
		if status == model.HealthHealthy {
			status = model.HealthDegraded
		}
		message = "management runtime responded but configuration could not be read"
	}
	fetchedAt := health.CheckedAt
	if readable {
		fetchedAt = snapshot.FetchedAt
	}
	return model.ResourceEnvelope[model.ConnectSetup]{
		Data: model.ConnectSetup{
			Source: model.SourceAgentGateway, ManagementConfigured: true, ConfigurationReadable: readable,
			Status: status, Version: health.Version, LatencyMS: health.LatencyMS, CheckedAt: health.CheckedAt,
			Message: message, Links: service.links,
		},
		Meta: gatewayMeta(fetchedAt, !readable || status != model.HealthHealthy),
	}
}

func count(id, label string, value int) model.ConnectCount {
	return model.ConnectCount{ID: id, Label: label, Value: &value, Status: "configured"}
}

func gatewayMeta(fetchedAt time.Time, partial bool) model.Meta {
	return model.Meta{Source: model.SourceAgentGateway, FetchedAt: fetchedAt, Stale: false, Partial: partial}
}

func detail[T any](items []T, id string, fetchedAt time.Time, idOf func(T) string) (model.ResourceEnvelope[T], error) {
	for _, item := range items {
		if idOf(item) == id {
			return model.ResourceEnvelope[T]{Data: item, Meta: gatewayMeta(fetchedAt, false)}, nil
		}
	}
	return model.ResourceEnvelope[T]{}, ErrNotFound
}

func filter[T any](items []T, query string, text func(T) string) []T {
	normalized := strings.ToLower(strings.TrimSpace(query))
	filtered := make([]T, 0, len(items))
	for _, item := range items {
		if normalized == "" || strings.Contains(strings.ToLower(text(item)), normalized) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func paginate[T any](items []T, cursor string, limit int) (model.ResourcePage[T], error) {
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 1 || limit > maxLimit {
		return model.ResourcePage[T]{}, fmt.Errorf("limit must be between 1 and %d", maxLimit)
	}
	offset, err := decodeCursor(cursor)
	if err != nil || offset > len(items) {
		return model.ResourcePage[T]{}, ErrInvalidCursor
	}
	end := min(offset+limit, len(items))
	page := model.ResourcePage[T]{Items: items[offset:end], Total: len(items)}
	if end < len(items) {
		next := encodeCursor(end)
		page.NextCursor = &next
	}
	return page, nil
}

func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	offset, err := strconv.Atoi(string(decoded))
	if err != nil || offset < 0 {
		return 0, ErrInvalidCursor
	}
	return offset, nil
}

func consoleLinks(raw string) model.ConsoleLinks {
	if raw == "" {
		return model.ConsoleLinks{}
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return model.ConsoleLinks{}
	}
	base := strings.TrimRight(parsed.String(), "/")
	return model.ConsoleLinks{
		Console: base, RawConfig: base + "/raw-config", CEL: base + "/cel",
		LLMPlayground: base + "/llm/playground", MCPPlayground: base + "/mcp/playground",
		GatewayLogs: base + "/llm/logs",
	}
}
