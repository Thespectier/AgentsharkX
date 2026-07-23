package protect

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

type fakeGateway struct {
	snapshot model.GatewaySnapshot
	err      error
}

func (fake fakeGateway) Snapshot(context.Context) (model.GatewaySnapshot, error) {
	return fake.snapshot, fake.err
}

type fakeGuard struct {
	mu              sync.Mutex
	trust           model.TrustSnapshot
	rules           []model.RuntimeRule
	approvals       []model.Approval
	plugins         model.ProtectPluginSnapshot
	rulesErr        error
	pluginsErr      error
	check           model.RuntimeRuleCheck
	checkErr        error
	publishedSource string
	publishedAgent  string
	publishCalls    int
	deleteCalls     int
	resolvedTicket  string
	resolvedAction  string
	resolvedNote    string
	resolveCalls    int
	blockResolve    chan struct{}
}

func (guard *fakeGuard) TrustSnapshot(context.Context) (model.TrustSnapshot, error) {
	return guard.trust, nil
}
func (guard *fakeGuard) RuntimeRules(context.Context) ([]model.RuntimeRule, time.Time, error) {
	return append([]model.RuntimeRule{}, guard.rules...), time.Now().UTC(), guard.rulesErr
}
func (guard *fakeGuard) CheckRuntimeRule(context.Context, string) (model.RuntimeRuleCheck, error) {
	return guard.check, guard.checkErr
}
func (guard *fakeGuard) PublishRuntimeRule(_ context.Context, agent, source string) (string, error) {
	guard.mu.Lock()
	defer guard.mu.Unlock()
	guard.publishCalls++
	guard.publishedAgent, guard.publishedSource = agent, source
	return "rule-upstream", nil
}
func (guard *fakeGuard) DeleteRuntimeRule(context.Context, string, string) error {
	guard.mu.Lock()
	guard.deleteCalls++
	guard.mu.Unlock()
	return nil
}
func (guard *fakeGuard) Approvals(context.Context) ([]model.Approval, time.Time, error) {
	return append([]model.Approval{}, guard.approvals...), time.Now().UTC(), nil
}
func (guard *fakeGuard) ResolveApproval(_ context.Context, ticket, action, note string) error {
	guard.mu.Lock()
	guard.resolveCalls++
	guard.resolvedTicket, guard.resolvedAction, guard.resolvedNote = ticket, action, note
	guard.mu.Unlock()
	if guard.blockResolve != nil {
		<-guard.blockResolve
	}
	return nil
}
func (guard *fakeGuard) ProtectPlugins(context.Context) (model.ProtectPluginSnapshot, error) {
	return guard.plugins, guard.pluginsErr
}

func TestSnapshotPreservesIndependentSourceFailure(t *testing.T) {
	t.Parallel()
	guard := &fakeGuard{
		rules:   []model.RuntimeRule{{ProtectResourceBase: model.ProtectResourceBase{ID: "rule"}}},
		plugins: model.ProtectPluginSnapshot{FetchedAt: time.Now().UTC()},
	}
	service := New(fakeGateway{err: errors.New("gateway down")}, guard, model.ConsoleLinks{})
	snapshot, err := service.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Meta.Partial || len(snapshot.Meta.SourceFailures) != 1 || snapshot.Meta.SourceFailures[0].Source != model.SourceAgentGateway {
		t.Fatalf("source failure was not preserved: %#v", snapshot.Meta)
	}
	if len(snapshot.Data.RuntimeRules) != 1 {
		t.Fatalf("AgentGuard success was suppressed: %#v", snapshot.Data)
	}
}

func TestSnapshotPreservesValidatedNativeConsoleLinks(t *testing.T) {
	t.Parallel()
	links := model.ConsoleLinks{
		RawConfig:         "http://localhost:15000/ui/raw-config",
		AgentGuardConsole: "http://localhost:38008",
	}
	service := New(
		fakeGateway{snapshot: model.GatewaySnapshot{FetchedAt: time.Now().UTC()}},
		&fakeGuard{plugins: model.ProtectPluginSnapshot{FetchedAt: time.Now().UTC()}},
		links,
	)
	snapshot, err := service.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Data.Links != links {
		t.Fatalf("native console links changed: %#v", snapshot.Data.Links)
	}
}

func TestRulePublishRequiresCurrentSuccessfulOneTimeCheck(t *testing.T) {
	t.Parallel()
	const source = "RULE safe\nACTION ALLOW"
	guard := &fakeGuard{
		trust: model.TrustSnapshot{Sessions: []model.TrustSession{{AgentID: "agent-opaque", AgentUpstreamID: "agent-a"}}},
		check: model.RuntimeRuleCheck{OK: true, RuleCount: 1, Errors: []model.RuleCheckMessage{}, Warnings: []model.RuleCheckMessage{}, Hints: []model.RuleCheckMessage{}},
	}
	service := New(fakeGateway{}, guard, model.ConsoleLinks{})
	request := model.RuntimeRulePublishRequest{Source: source, Note: " reviewed ", Confirmed: true}
	if _, err := service.PublishRule(t.Context(), "agent-opaque", request); !errors.Is(err, ErrRuleCheckRequired) {
		t.Fatalf("publish without check error = %v", err)
	}
	check, err := service.CheckRule(t.Context(), source)
	if err != nil || !check.Publishable || check.CheckToken == "" || check.ExpiresAt == nil {
		t.Fatalf("unexpected check: %#v err=%v", check, err)
	}
	request.CheckToken = check.CheckToken
	request.Source += " changed"
	if _, err := service.PublishRule(t.Context(), "agent-opaque", request); !errors.Is(err, ErrRuleCheckRequired) {
		t.Fatalf("source-bound check error = %v", err)
	}
	request.Source = source
	receipt, err := service.PublishRule(t.Context(), "agent-opaque", request)
	if err != nil || receipt.Data.Operation != "publish-runtime-rule" || guard.publishCalls != 1 || guard.publishedAgent != "agent-a" {
		t.Fatalf("unexpected publish: %#v guard=%#v err=%v", receipt, guard, err)
	}
	if _, err := service.PublishRule(t.Context(), "agent-opaque", request); !errors.Is(err, ErrRuleCheckRequired) {
		t.Fatalf("reused check error = %v", err)
	}
}

func TestInvalidRuleCheckCannotBePublished(t *testing.T) {
	t.Parallel()
	guard := &fakeGuard{check: model.RuntimeRuleCheck{
		OK: false, RuleCount: 0,
		Errors: []model.RuleCheckMessage{{Message: "invalid rule"}}, Warnings: []model.RuleCheckMessage{}, Hints: []model.RuleCheckMessage{},
	}}
	service := New(fakeGateway{}, guard, model.ConsoleLinks{})
	check, err := service.CheckRule(t.Context(), "invalid")
	if err != nil || check.Publishable || check.CheckToken != "" {
		t.Fatalf("unexpected invalid check: %#v err=%v", check, err)
	}
}

func TestDeleteOnlyCurrentUserManagedRule(t *testing.T) {
	t.Parallel()
	guard := &fakeGuard{rules: []model.RuntimeRule{{
		ProtectResourceBase: model.ProtectResourceBase{ID: "rule-opaque", UpstreamID: "rule-a"},
		AgentID:             "agent-opaque", AgentUpstreamID: "agent-a", UserManaged: true,
	}}}
	service := New(fakeGateway{}, guard, model.ConsoleLinks{})
	request := model.ConfirmedActionRequest{Note: "retired", Confirmed: true}
	receipt, err := service.DeleteRule(t.Context(), "agent-opaque", "rule-opaque", request)
	if err != nil || receipt.Data.Operation != "delete-runtime-rule" || guard.deleteCalls != 1 {
		t.Fatalf("unexpected delete: %#v err=%v", receipt, err)
	}
	guard.rules[0].UserManaged = false
	if _, err := service.DeleteRule(t.Context(), "agent-opaque", "rule-opaque", request); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-user-managed delete error = %v", err)
	}
}

func TestApprovalRequiresNoteAndPreventsDuplicateInFlightDecision(t *testing.T) {
	t.Parallel()
	block := make(chan struct{})
	guard := &fakeGuard{
		approvals:    []model.Approval{{ProtectResourceBase: model.ProtectResourceBase{ID: "ticket-opaque", UpstreamID: "ticket-a"}}},
		blockResolve: block,
	}
	service := New(fakeGateway{}, guard, model.ConsoleLinks{})
	if _, err := service.ResolveApproval(t.Context(), "ticket-opaque", "approve", model.ConfirmedActionRequest{Confirmed: true}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing note error = %v", err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := service.ResolveApproval(context.Background(), "ticket-opaque", "approve", model.ConfirmedActionRequest{Note: "reviewed", Confirmed: true})
		result <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		guard.mu.Lock()
		calls := guard.resolveCalls
		guard.mu.Unlock()
		if calls == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first approval mutation did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := service.ResolveApproval(t.Context(), "ticket-opaque", "deny", model.ConfirmedActionRequest{Note: "duplicate", Confirmed: true}); !errors.Is(err, ErrMutationInFlight) {
		t.Fatalf("duplicate decision error = %v", err)
	}
	close(block)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if guard.resolveCalls != 1 || guard.resolvedTicket != "ticket-a" || guard.resolvedAction != "approve" || guard.resolvedNote != "reviewed" {
		t.Fatalf("unexpected approval call: %#v", guard)
	}
}
