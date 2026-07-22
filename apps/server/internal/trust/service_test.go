package trust

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/upstream"
)

type fakeGuard struct {
	snapshot       model.TrustSnapshot
	update         model.TrustResource
	detectResults  []model.TrustDetection
	detectWarnings []string
	detectErr      error
	started        chan struct{}
	release        chan struct{}
	mu             sync.Mutex
	updates        []model.TrustLabelUpdate
}

func (fake *fakeGuard) TrustSnapshot(context.Context) (model.TrustSnapshot, error) {
	return fake.snapshot, nil
}

func (fake *fakeGuard) UpdateToolLabels(_ context.Context, _, _ string, update model.TrustLabelUpdate) (model.TrustResource, error) {
	fake.mu.Lock()
	fake.updates = append(fake.updates, update)
	fake.mu.Unlock()
	return fake.update, nil
}

func (fake *fakeGuard) DetectSkills(ctx context.Context, _ string, _ []string, _ bool) ([]model.TrustDetection, []string, error) {
	return fake.detect(ctx)
}

func (fake *fakeGuard) DetectMCPs(ctx context.Context, _ string, _ []string) ([]model.TrustDetection, []string, error) {
	return fake.detect(ctx)
}

func (fake *fakeGuard) detect(ctx context.Context) ([]model.TrustDetection, []string, error) {
	if fake.started != nil {
		select {
		case fake.started <- struct{}{}:
		default:
		}
	}
	if fake.release != nil {
		select {
		case <-fake.release:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	return fake.detectResults, fake.detectWarnings, fake.detectErr
}

func TestAgentsUseOnlyExplicitAgentGuardIDsAndKeepUnknownIdentityFields(t *testing.T) {
	t.Parallel()
	fetchedAt := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	agentID := "opaque-agent-a"
	fake := &fakeGuard{snapshot: model.TrustSnapshot{
		FetchedAt: fetchedAt,
		Failures:  []model.SourceFailure{{Source: model.SourceAgentGuard, Code: "UPSTREAM_UNAVAILABLE", Message: "MCP unavailable"}},
		Sessions: []model.TrustSession{
			{TrustResourceBase: model.TrustResourceBase{ID: "session-1"}, AgentID: agentID, AgentUpstreamID: "agent-a", UserID: "user-a", LastSeen: &fetchedAt},
			{TrustResourceBase: model.TrustResourceBase{ID: "session-no-agent"}, AgentID: "", AgentUpstreamID: ""},
		},
		Resources: []model.TrustResource{
			trustResource("tool-1", "mail.send", "tool", agentID, "agent-a", ""),
			trustResource("skill-1", "research", "skill", agentID, "agent-a", "langchain"),
		},
	}}
	service := New(t.Context(), fake, time.Second)

	page, err := service.Agents(t.Context(), "agent-a", "", 25)
	if err != nil {
		t.Fatal(err)
	}
	if page.Data.Total != 1 || !page.Meta.Partial {
		t.Fatalf("unexpected agent page: %#v", page)
	}
	agent := page.Data.Items[0]
	if agent.UpstreamID != "agent-a" || agent.Framework == nil || *agent.Framework != "langchain" {
		t.Fatalf("explicit identity fields were not preserved: %#v", agent)
	}
	if agent.Principal != nil || agent.TrustLevel != nil || agent.Status != "unknown" || agent.Sessions != 1 || agent.Tools != 1 || agent.Skills != 1 {
		t.Fatalf("unknown identity fields were invented: %#v", agent)
	}
	workspace, err := service.Agent(t.Context(), agentID)
	if err != nil || len(workspace.Data.Sessions) != 1 || len(workspace.Data.Resources) != 2 {
		t.Fatalf("unexpected workspace: %#v err=%v", workspace, err)
	}
}

func TestResourcesFilterPaginateAndRejectInvalidType(t *testing.T) {
	t.Parallel()
	fetchedAt := time.Now().UTC()
	fake := &fakeGuard{snapshot: model.TrustSnapshot{FetchedAt: fetchedAt, Resources: []model.TrustResource{
		trustResource("t1", "mail.send", "tool", "a1", "agent-a", ""),
		trustResource("s1", "web research", "skill", "a1", "agent-a", "langchain"),
		trustResource("s2", "database research", "skill", "a2", "agent-b", "autogen"),
	}}}
	service := New(t.Context(), fake, time.Second)
	first, err := service.Resources(t.Context(), "research", "skill", "", "", 1)
	if err != nil || first.Data.Total != 2 || first.Data.NextCursor == nil {
		t.Fatalf("unexpected first page: %#v err=%v", first, err)
	}
	second, err := service.Resources(t.Context(), "research", "skill", "", *first.Data.NextCursor, 1)
	if err != nil || len(second.Data.Items) != 1 || second.Data.Items[0].ID != "s2" {
		t.Fatalf("unexpected second page: %#v err=%v", second, err)
	}
	if _, err := service.Resources(t.Context(), "", "gateway-mcp", "", "", 25); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid type error, got %v", err)
	}
}

func TestToolLabelUpdateUsesServerResponseAsFinalFact(t *testing.T) {
	t.Parallel()
	original := trustResource("tool-1", "mail.send", "tool", "agent-1", "agent-a", "")
	original.Labels = &model.TrustLabels{Boundary: "internal", Sensitivity: "low", Integrity: "trusted", Tags: []string{}}
	updated := original
	updated.Labels = &model.TrustLabels{Boundary: "server-confirmed", Sensitivity: "low", Integrity: "trusted", Tags: []string{"approved"}}
	fake := &fakeGuard{snapshot: model.TrustSnapshot{FetchedAt: time.Now().UTC(), Resources: []model.TrustResource{original}}, update: updated}
	service := New(t.Context(), fake, time.Second)
	boundary := "external"
	response, err := service.UpdateToolLabels(t.Context(), "agent-1", "tool-1", model.TrustLabelUpdate{Boundary: &boundary})
	if err != nil {
		t.Fatal(err)
	}
	if response.Data.Labels == nil || response.Data.Labels.Boundary != "server-confirmed" || len(fake.updates) != 1 {
		t.Fatalf("server response was not final: %#v", response.Data)
	}
}

func TestScanJobExposesRealStateTransitionsAndResults(t *testing.T) {
	t.Parallel()
	resource := trustResource("skill-1", "research", "skill", "agent-1", "agent-a", "langchain")
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	fake := &fakeGuard{
		snapshot:      model.TrustSnapshot{FetchedAt: time.Now().UTC(), Resources: []model.TrustResource{resource}},
		detectResults: []model.TrustDetection{{ResourceUpstreamID: "research-upstream", Name: "research", Label: "benign", RiskLevel: "low"}},
		started:       started, release: release,
	}
	service := New(t.Context(), fake, time.Second)
	created, err := service.StartScan(t.Context(), "agent-1", "skill", model.TrustDetectionRequest{ResourceIDs: []string{"skill-1"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("scan did not start")
	}
	running, err := service.ScanJob(created.Data.ID)
	if err != nil || running.Data.Status != "running" || running.Data.StartedAt == nil {
		t.Fatalf("unexpected running job: %#v err=%v", running, err)
	}
	close(release)
	completed := waitForJob(t, service, created.Data.ID, "succeeded")
	if len(completed.Results) != 1 || completed.Results[0].Label != "benign" || completed.CompletedAt == nil {
		t.Fatalf("unexpected completed job: %#v", completed)
	}
}

func TestScanJobPreservesRecoverableFailureWithoutSensitiveBody(t *testing.T) {
	t.Parallel()
	resource := trustResource("mcp-1", "inventory", "mcp", "agent-1", "agent-a", "mcp_native")
	fake := &fakeGuard{
		snapshot:  model.TrustSnapshot{FetchedAt: time.Now().UTC(), Resources: []model.TrustResource{resource}},
		detectErr: &upstream.Error{Source: model.SourceAgentGuard, Method: "POST", Path: "/detect", Status: 503, Retryable: true, Kind: "sensitive body is never stored"},
	}
	service := New(t.Context(), fake, time.Second)
	created, err := service.StartScan(t.Context(), "agent-1", "mcp", model.TrustDetectionRequest{ResourceIDs: []string{"mcp-1"}})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitForJob(t, service, created.Data.ID, "failed")
	if failed.Error == nil || failed.Error.Code != "UPSTREAM_UNAVAILABLE" || !failed.Error.Retryable || failed.Error.Message == "sensitive body is never stored" {
		t.Fatalf("unexpected safe scan error: %#v", failed.Error)
	}
}

func waitForJob(t *testing.T, service *Service, id, status string) model.TrustScanJob {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		job, err := service.ScanJob(id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Data.Status == status {
			return job.Data
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("job %s did not reach %s", id, status)
	return model.TrustScanJob{}
}

func trustResource(id, name, resourceType, agentID, upstreamAgentID, framework string) model.TrustResource {
	return model.TrustResource{
		TrustResourceBase: model.TrustResourceBase{ID: id, UpstreamID: name, Source: model.SourceAgentGuard, FetchedAt: time.Now().UTC()},
		Name:              name, Type: resourceType, OwnerAgentID: agentID, OwnerAgentUpstreamID: upstreamAgentID, Framework: framework,
	}
}
