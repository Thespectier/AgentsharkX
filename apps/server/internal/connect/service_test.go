package connect

import (
	"context"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

type fakeGateway struct {
	snapshot  model.GatewaySnapshot
	analytics model.GatewayAnalytics
	health    model.SourceHealth
}

func (fake fakeGateway) Health(context.Context) model.SourceHealth { return fake.health }
func (fake fakeGateway) Snapshot(context.Context) (model.GatewaySnapshot, error) {
	return fake.snapshot, nil
}
func (fake fakeGateway) Analytics(context.Context) (model.GatewayAnalytics, error) {
	return fake.analytics, nil
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
	if summary.Data.Links.RawConfig != "http://localhost:15000/ui/raw-config" || summary.Data.Links.CEL != "http://localhost:15000/ui/cel" {
		t.Fatalf("unexpected console links: %#v", summary.Data.Links)
	}
	setup := service.Setup(t.Context())
	if !setup.Data.ConfigurationReadable || setup.Data.Version != "1.3.1" || setup.Data.Status != model.HealthHealthy {
		t.Fatalf("unexpected setup verification: %#v", setup.Data)
	}
}
