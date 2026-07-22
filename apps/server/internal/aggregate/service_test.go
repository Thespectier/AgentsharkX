package aggregate

import (
	"context"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

type fakeSource struct {
	health       model.SourceHealth
	capabilities []model.Capability
}

func (source fakeSource) Health(context.Context) model.SourceHealth { return source.health }
func (source fakeSource) Capabilities(context.Context) []model.Capability {
	return source.capabilities
}

func TestRefreshPreservesHealthySourceWhenPeerIsDown(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	service := New("test", fakeSource{health: model.SourceHealth{
		Source: model.SourceAgentGateway, Status: model.HealthHealthy, CheckedAt: now,
	}}, fakeSource{health: model.SourceHealth{
		Source: model.SourceAgentGuard, Status: model.HealthDown, CheckedAt: now, Message: "upstream unavailable",
	}})

	health := service.Refresh(t.Context())
	if len(health) != 2 || health[0].Status != model.HealthHealthy || health[1].Status != model.HealthDown {
		t.Fatalf("independent health was not preserved: %#v", health)
	}
	overview := service.Overview()
	if !overview.Meta.Partial || len(overview.Meta.SourceFailures) != 1 {
		t.Fatalf("expected partial overview with one source failure: %#v", overview.Meta)
	}
	if len(overview.Data.Metrics) != 0 || len(overview.Data.Events) != 0 {
		t.Fatalf("Phase 2 overview must not fabricate traffic data: %#v", overview.Data)
	}
}
