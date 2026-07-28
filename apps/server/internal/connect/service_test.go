package connect

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

type fakeGateway struct {
	snapshot             model.GatewaySnapshot
	analytics            model.GatewayAnalytics
	health               model.SourceHealth
	configuration        model.LLMConfiguration
	mcpConfiguration     model.MCPConfiguration
	trafficConfiguration model.TrafficConfiguration
	policyConfiguration  model.GatewayPolicyConfiguration
	revision             string
	apply                func(context.Context, string, model.LLMChange) error
	applyMCP             func(context.Context, string, model.MCPChange) error
	applyTraffic         func(context.Context, string, model.TrafficChange) error
	applyPolicy          func(context.Context, string, model.GatewayPolicyChange) error
}

func (fake fakeGateway) Health(context.Context) model.SourceHealth { return fake.health }
func (fake fakeGateway) Snapshot(context.Context) (model.GatewaySnapshot, error) {
	return fake.snapshot, nil
}
func (fake fakeGateway) Analytics(context.Context) (model.GatewayAnalytics, error) {
	return fake.analytics, nil
}
func (fake fakeGateway) LLMConfiguration(context.Context) (model.LLMConfiguration, string, error) {
	return fake.configuration, fake.revision, nil
}
func (fake fakeGateway) ApplyLLMChange(ctx context.Context, revision string, change model.LLMChange) (string, error) {
	if fake.apply != nil {
		if err := fake.apply(ctx, revision, change); err != nil {
			return "", err
		}
	}
	if change.Provider.Name != "" {
		return change.Provider.Name, nil
	}
	if change.Model.Name != "" {
		return change.Model.Name, nil
	}
	return change.ResourceID, nil
}
func (fake fakeGateway) MCPConfiguration(context.Context) (model.MCPConfiguration, string, error) {
	return fake.mcpConfiguration, fake.revision, nil
}
func (fake fakeGateway) ApplyMCPChange(ctx context.Context, revision string, change model.MCPChange) (string, error) {
	if fake.applyMCP != nil {
		if err := fake.applyMCP(ctx, revision, change); err != nil {
			return "", err
		}
	}
	if change.Server.Name != "" {
		return change.Server.Name, nil
	}
	if change.Operation == "update-mcp-settings" {
		return "MCP settings", nil
	}
	return change.ResourceID, nil
}
func (fake fakeGateway) TrafficConfiguration(context.Context) (model.TrafficConfiguration, string, error) {
	return fake.trafficConfiguration, fake.revision, nil
}
func (fake fakeGateway) ApplyTrafficChange(ctx context.Context, revision string, change model.TrafficChange) (string, error) {
	if fake.applyTraffic != nil {
		if err := fake.applyTraffic(ctx, revision, change); err != nil {
			return "", err
		}
	}
	return change.ResourceID, nil
}
func (fake fakeGateway) PolicyConfiguration(context.Context) (model.GatewayPolicyConfiguration, string, error) {
	return fake.policyConfiguration, fake.revision, nil
}
func (fake fakeGateway) ApplyPolicyChange(ctx context.Context, revision string, change model.GatewayPolicyChange) (string, error) {
	if fake.applyPolicy != nil {
		if err := fake.applyPolicy(ctx, revision, change); err != nil {
			return "", err
		}
	}
	return change.ResourceID, nil
}

func TestResourceListsFilterPaginateAndResolveDetails(t *testing.T) {
	t.Parallel()
	fetchedAt := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	provider := func(id, name, kind string) model.GatewayProvider {
		return model.GatewayProvider{ConnectResource: model.ConnectResource{
			ID: id, UpstreamID: name, Source: model.SourceAgentGateway, FetchedAt: fetchedAt,
			RawRef: model.RawRef{Source: model.SourceAgentGateway, ID: "/llm/providers/" + id},
		}, Name: name, Kind: kind}
	}
	service := New(fakeGateway{snapshot: model.GatewaySnapshot{
		FetchedAt: fetchedAt,
		Providers: []model.GatewayProvider{
			provider("p1", "alpha", "openai"), provider("p2", "beta", "anthropic"), provider("p3", "gamma", "openai"),
		},
	}}, "http://localhost:15000/ui")

	first, err := service.Providers(t.Context(), "openai", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Data.Total != 2 || len(first.Data.Items) != 1 || first.Data.NextCursor == nil {
		t.Fatalf("unexpected first page: %#v", first.Data)
	}
	second, err := service.Providers(t.Context(), "openai", *first.Data.NextCursor, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Data.Items) != 1 || second.Data.Items[0].ID != "p3" || second.Data.NextCursor != nil {
		t.Fatalf("unexpected second page: %#v", second.Data)
	}
	detail, err := service.Provider(t.Context(), "p2")
	if err != nil || detail.Data.UpstreamID != "beta" {
		t.Fatalf("unexpected detail: data=%#v err=%v", detail.Data, err)
	}
	if _, err := service.Providers(t.Context(), "", "not-a-cursor", 25); err != ErrInvalidCursor {
		t.Fatalf("expected invalid cursor, got %v", err)
	}
}

func TestSummaryAndSetupExposeVerifiedLinksWithoutInventingHealth(t *testing.T) {
	t.Parallel()
	fetchedAt := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	requests := int64(12)
	service := New(fakeGateway{
		snapshot:  model.GatewaySnapshot{FetchedAt: fetchedAt, Listeners: 2, Backends: 3, Routes: make([]model.GatewayRoute, 1)},
		analytics: model.GatewayAnalytics{Status: "available", Requests: &requests, Buckets: []model.AnalyticsBucket{}},
		health:    model.SourceHealth{Source: model.SourceAgentGateway, Status: model.HealthHealthy, Version: "1.3.1", CheckedAt: fetchedAt},
	}, "http://localhost:15000/ui")

	summary, err := service.Summary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Data.Counts) != 4 || *summary.Data.Counts[0].Value != 2 || summary.Data.Counts[0].Status != "configured" {
		t.Fatalf("unexpected counts: %#v", summary.Data.Counts)
	}
	if summary.Data.Links.RawConfig != "http://localhost:15000/ui/raw-config" ||
		summary.Data.Links.CEL != "http://localhost:15000/ui/cel" ||
		summary.Data.Links.GatewayLogs != "http://localhost:15000/ui/llm/logs" {
		t.Fatalf("unexpected console links: %#v", summary.Data.Links)
	}
	setup := service.Setup(t.Context())
	if !setup.Data.ConfigurationReadable || setup.Data.Version != "1.3.1" || setup.Data.Status != model.HealthHealthy {
		t.Fatalf("unexpected setup verification: %#v", setup.Data)
	}
}

func TestLLMConfigurationIssuesOneTimeRevisionAndAppliesTypedChange(t *testing.T) {
	t.Parallel()
	fetchedAt := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	var applied model.LLMChange
	service := New(fakeGateway{
		configuration: model.LLMConfiguration{
			Source: model.SourceAgentGateway, FetchedAt: fetchedAt,
			Providers: []model.LLMProviderSetting{}, Models: []model.LLMModelSetting{},
			VirtualModels: []model.GatewayModel{},
		},
		revision: "revision-a",
		apply: func(_ context.Context, revision string, change model.LLMChange) error {
			if revision != "revision-a" {
				t.Fatalf("revision = %q", revision)
			}
			applied = change
			return nil
		},
	}, "http://localhost:15000/ui")

	configuration, err := service.LLMConfiguration(t.Context())
	if err != nil || configuration.Data.RevisionToken == "" || configuration.Data.Links.RawConfig == "" {
		t.Fatalf("unexpected configuration: %#v err=%v", configuration, err)
	}
	request := model.LLMProviderMutationRequest{
		RevisionToken: configuration.Data.RevisionToken,
		Provider: model.LLMProviderDraft{
			Name: "shared", ProviderType: "openai", Formats: []model.LLMProviderFormat{},
			Credential: model.LLMCredentialInput{Mode: "environment", Reference: "OPENAI_API_KEY"},
		},
	}
	receipt, err := service.CreateProvider(t.Context(), request)
	if err != nil || receipt.Data.Operation != "create-llm-provider" || applied.Provider.Name != "shared" {
		t.Fatalf("unexpected mutation: receipt=%#v change=%#v err=%v", receipt, applied, err)
	}
	if _, err := service.CreateProvider(t.Context(), request); !errors.Is(err, ErrRevisionStale) {
		t.Fatalf("one-time revision error = %v", err)
	}
	configuration, err = service.LLMConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeleteProvider(t.Context(), "provider-1", model.LLMProviderDeleteRequest{
		RevisionToken: configuration.Data.RevisionToken,
		Confirmed:     true,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing cascade choice error = %v", err)
	}
	deleteReferencedModels := true
	receipt, err = service.DeleteProvider(t.Context(), "provider-1", model.LLMProviderDeleteRequest{
		RevisionToken:          configuration.Data.RevisionToken,
		Confirmed:              true,
		DeleteReferencedModels: &deleteReferencedModels,
	})
	if err != nil || receipt.Data.Operation != "delete-llm-provider" || !applied.DeleteReferencedModels {
		t.Fatalf("unexpected cascade deletion: receipt=%#v change=%#v err=%v", receipt, applied, err)
	}
}

func TestMCPConfigurationIssuesOneTimeRevisionAndAppliesTypedChange(t *testing.T) {
	t.Parallel()
	fetchedAt := time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC)
	var applied model.MCPChange
	service := New(fakeGateway{
		mcpConfiguration: model.MCPConfiguration{
			Source: model.SourceAgentGateway, FetchedAt: fetchedAt,
			Settings: model.MCPGlobalSettings{StatefulMode: "stateless", PrefixMode: "none", FailureMode: "failClosed"},
			Servers:  []model.MCPServerSetting{}, InlineServers: []model.GatewayMCPServer{},
		},
		revision: "revision-mcp-a",
		applyMCP: func(_ context.Context, revision string, change model.MCPChange) error {
			if revision != "revision-mcp-a" {
				t.Fatalf("revision = %q", revision)
			}
			applied = change
			return nil
		},
	}, "http://localhost:15000/ui")

	configuration, err := service.MCPConfiguration(t.Context())
	if err != nil || configuration.Data.RevisionToken == "" || configuration.Data.Links.MCPPlayground == "" {
		t.Fatalf("unexpected MCP configuration: %#v err=%v", configuration, err)
	}
	request := model.MCPServerMutationRequest{
		RevisionToken: configuration.Data.RevisionToken,
		Server: model.MCPServerDraft{
			Name: "weather", Transport: "mcp",
			Network: &model.MCPNetworkTarget{Mode: "url", Host: "https://weather.example/mcp"},
		},
	}
	receipt, err := service.CreateMCPServer(t.Context(), request)
	if err != nil || receipt.Data.Operation != "create-mcp-server" || applied.Server.Name != "weather" {
		t.Fatalf("unexpected MCP mutation: receipt=%#v change=%#v err=%v", receipt, applied, err)
	}
	if _, err := service.CreateMCPServer(t.Context(), request); !errors.Is(err, ErrRevisionStale) {
		t.Fatalf("one-time revision error = %v", err)
	}
}

func TestTrafficConfigurationIssuesOneTimeRevisionAndAppliesChange(t *testing.T) {
	t.Parallel()
	fetchedAt := time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)
	var applied model.TrafficChange
	service := New(fakeGateway{
		trafficConfiguration: model.TrafficConfiguration{
			Source: model.SourceAgentGateway, FetchedAt: fetchedAt,
			Binds: []model.TrafficBindSetting{}, Listeners: []model.TrafficListenerSetting{}, Routes: []model.TrafficRouteSetting{},
		},
		revision: "revision-traffic-a",
		applyTraffic: func(_ context.Context, revision string, change model.TrafficChange) error {
			if revision != "revision-traffic-a" {
				t.Fatalf("revision = %q", revision)
			}
			applied = change
			return nil
		},
	}, "http://localhost:15000/ui")

	configuration, err := service.TrafficConfiguration(t.Context())
	if err != nil || configuration.Data.RevisionToken == "" || configuration.Data.Links.RawConfig == "" {
		t.Fatalf("unexpected Traffic configuration: %#v err=%v", configuration, err)
	}
	request := model.TrafficRouteMutationRequest{
		RevisionToken: configuration.Data.RevisionToken,
		ListenerID:    "listener-1",
		Route: model.TrafficRouteDraft{
			Kind: "http",
			Configuration: model.TrafficConfigObject{
				"name": "api", "matches": []any{map[string]any{"path": map[string]any{"pathPrefix": "/"}}},
			},
		},
	}
	receipt, err := service.CreateTrafficRoute(t.Context(), request)
	if err != nil || receipt.Data.Operation != "create-traffic-route" || applied.ListenerID != "listener-1" {
		t.Fatalf("unexpected Traffic mutation: receipt=%#v change=%#v err=%v", receipt, applied, err)
	}
	if _, err := service.CreateTrafficRoute(t.Context(), request); !errors.Is(err, ErrRevisionStale) {
		t.Fatalf("one-time revision error = %v", err)
	}
}

func TestGatewayPolicyConfigurationIssuesOneTimeRevisionAndAppliesCompleteValue(t *testing.T) {
	t.Parallel()
	fetchedAt := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	var applied model.GatewayPolicyChange
	service := New(fakeGateway{
		policyConfiguration: model.GatewayPolicyConfiguration{
			Source: model.SourceAgentGateway, FetchedAt: fetchedAt,
			Settings: []model.GatewayPolicySetting{{
				ProtectResourceBase: model.ProtectResourceBase{
					ID: "policy-1", UpstreamID: "/llm/policies/cors", Source: model.SourceAgentGateway,
					FetchedAt: fetchedAt, RawRef: model.RawRef{Source: model.SourceAgentGateway, ID: "/llm/policies/cors"},
				},
				Family: "llm", Target: "LLM Gateway", Key: "cors", Title: "CORS", Group: "Access",
				Description: "CORS policy", Scope: "Gateway", Phase: "Request", Action: "Authorize",
				Enabled: true, Editable: true, Value: map[string]any{"allowOrigins": []any{"https://admin.example"}},
			}},
		},
		revision: "revision-policy-a",
		applyPolicy: func(_ context.Context, revision string, change model.GatewayPolicyChange) error {
			if revision != "revision-policy-a" {
				t.Fatalf("revision = %q", revision)
			}
			applied = change
			return nil
		},
	}, "http://localhost:15000/ui")

	configuration, err := service.GatewayPolicyConfiguration(t.Context())
	if err != nil || configuration.Data.RevisionToken == "" || configuration.Data.Links.RawConfig == "" || len(configuration.Data.Settings) != 1 {
		t.Fatalf("unexpected gateway policy configuration: %#v err=%v", configuration, err)
	}
	value := []any{map[string]any{"requests": 100}}
	receipt, err := service.UpsertGatewayPolicy(t.Context(), "policy-1", model.GatewayPolicyMutationRequest{
		RevisionToken: configuration.Data.RevisionToken, Value: value,
	})
	if err != nil || receipt.Data.Operation != "upsert-gateway-policy" || applied.ResourceID != "policy-1" {
		t.Fatalf("unexpected gateway policy mutation: receipt=%#v change=%#v err=%v", receipt, applied, err)
	}
	if _, ok := applied.Value.([]any); !ok {
		t.Fatalf("complete array policy value was not preserved: %#v", applied.Value)
	}
	if _, err := service.UpsertGatewayPolicy(t.Context(), "policy-1", model.GatewayPolicyMutationRequest{
		RevisionToken: configuration.Data.RevisionToken, Value: value,
	}); !errors.Is(err, ErrRevisionStale) {
		t.Fatalf("one-time revision error = %v", err)
	}
}

func TestGatewayPolicyMutationSharesConfigurationLock(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	service := New(fakeGateway{
		policyConfiguration: model.GatewayPolicyConfiguration{Source: model.SourceAgentGateway, FetchedAt: time.Now().UTC(), Settings: []model.GatewayPolicySetting{}},
		configuration: model.LLMConfiguration{
			Source: model.SourceAgentGateway, FetchedAt: time.Now().UTC(),
			Providers: []model.LLMProviderSetting{}, Models: []model.LLMModelSetting{}, VirtualModels: []model.GatewayModel{},
		},
		revision: "shared-revision",
		applyPolicy: func(context.Context, string, model.GatewayPolicyChange) error {
			close(started)
			<-release
			return nil
		},
	}, "http://localhost:15000/ui")

	policyConfiguration, err := service.GatewayPolicyConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	llmConfiguration, err := service.LLMConfiguration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	mutationDone := make(chan error, 1)
	go func() {
		_, mutationErr := service.UpsertGatewayPolicy(context.Background(), "policy-1", model.GatewayPolicyMutationRequest{
			RevisionToken: policyConfiguration.Data.RevisionToken, Value: map[string]any{},
		})
		mutationDone <- mutationErr
	}()
	<-started
	_, err = service.CreateProvider(t.Context(), model.LLMProviderMutationRequest{
		RevisionToken: llmConfiguration.Data.RevisionToken,
		Provider: model.LLMProviderDraft{
			Name: "shared", ProviderType: "openai", Formats: []model.LLMProviderFormat{},
			Credential: model.LLMCredentialInput{Mode: "ambient"},
		},
	})
	if !errors.Is(err, ErrMutationInFlight) {
		t.Fatalf("concurrent LLM mutation error = %v", err)
	}
	close(release)
	if err := <-mutationDone; err != nil {
		t.Fatalf("gateway policy mutation error = %v", err)
	}
}
