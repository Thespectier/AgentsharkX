package aggregate

import (
	"context"
	"strings"
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
		t.Fatalf("health-only overview must not fabricate traffic data: %#v", overview.Data)
	}
}

func TestDiagnosticsGiveSourceSpecificRecoveryChecks(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	service := New("test", fakeSource{health: model.SourceHealth{
		Source: model.SourceAgentGateway, Label: "agentgateway", Status: model.HealthDown, CheckedAt: now,
	}}, fakeSource{health: model.SourceHealth{
		Source: model.SourceAgentGuard, Label: "AgentGuard", Status: model.HealthHealthy, CheckedAt: now,
	}})
	service.Refresh(t.Context())

	diagnostics := service.Diagnostics()
	if diagnostics.Data.Status != model.HealthDegraded || len(diagnostics.Data.Issues) != 1 {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
	issue := diagnostics.Data.Issues[0]
	if issue.Source != model.SourceAgentGateway || len(issue.Checks) < 4 {
		t.Fatalf("gateway recovery checks missing: %#v", issue)
	}
	for _, check := range issue.Checks {
		if strings.Contains(strings.ToLower(check), "token") {
			t.Fatalf("gateway diagnostics must not suggest an unused token: %q", check)
		}
	}
}

func TestDiagnosticsReportBothDisconnectedSources(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	service := New("test", fakeSource{health: model.SourceHealth{
		Source: model.SourceAgentGateway, Status: model.HealthDown, CheckedAt: now,
	}}, fakeSource{health: model.SourceHealth{
		Source: model.SourceAgentGuard, Status: model.HealthUnknown, CheckedAt: now,
	}})
	service.Refresh(t.Context())

	diagnostics := service.Diagnostics()
	if diagnostics.Data.Status != model.HealthDown || len(diagnostics.Data.Issues) != 2 {
		t.Fatalf("unexpected disconnected diagnostics: %#v", diagnostics)
	}
	if !strings.Contains(strings.Join(diagnostics.Data.Issues[1].Checks, " "), "AGENTGUARD_ADMIN_TOKEN") {
		t.Fatalf("AgentGuard credential recovery check missing: %#v", diagnostics.Data.Issues[1])
	}
}
