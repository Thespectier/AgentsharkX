// Package protect implements source-preserving policies and guarded AgentGuard mutations.
package protect

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/gateway"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/guard"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/upstream"
)

const (
	defaultLimit   = 25
	maxLimit       = 100
	maxSourceBytes = 12 * 1024
	maxNoteBytes   = 500
	maxCheckTokens = 100
	checkTokenTTL  = 5 * time.Minute
)

var (
	ErrInvalidCursor     = errors.New("invalid pagination cursor")
	ErrInvalidRequest    = errors.New("invalid protect request")
	ErrNotFound          = errors.New("protect resource not found")
	ErrRuleCheckRequired = errors.New("a current successful rule check is required")
	ErrMutationInFlight  = errors.New("protect mutation already in progress")
)

type Gateway interface {
	Snapshot(context.Context) (model.GatewaySnapshot, error)
}

type Guard interface {
	TrustSnapshot(context.Context) (model.TrustSnapshot, error)
	RuntimeRules(context.Context) ([]model.RuntimeRule, time.Time, error)
	CheckRuntimeRule(context.Context, string) (model.RuntimeRuleCheck, error)
	PublishRuntimeRule(context.Context, string, string) (string, error)
	DeleteRuntimeRule(context.Context, string, string) error
	Approvals(context.Context) ([]model.Approval, time.Time, error)
	ResolveApproval(context.Context, string, string, string) error
	ProtectPlugins(context.Context) (model.ProtectPluginSnapshot, error)
}

type checkedSource struct {
	digest    [sha256.Size]byte
	expiresAt time.Time
	createdAt time.Time
}

type Service struct {
	gateway Gateway
	guard   Guard
	links   model.ConsoleLinks
	mu      sync.Mutex
	checks  map[string]checkedSource
	active  map[string]struct{}
}

func New(gatewayClient Gateway, guardClient Guard, links model.ConsoleLinks) *Service {
	return &Service{
		gateway: gatewayClient, guard: guardClient, links: links,
		checks: make(map[string]checkedSource), active: make(map[string]struct{}),
	}
}

func (service *Service) Snapshot(ctx context.Context) (model.ProtectSnapshotEnvelope, error) {
	type gatewayResult struct {
		snapshot model.GatewaySnapshot
		err      error
	}
	type ruleResult struct {
		items     []model.RuntimeRule
		fetchedAt time.Time
		err       error
	}
	type pluginResult struct {
		snapshot model.ProtectPluginSnapshot
		err      error
	}
	var gatewayData gatewayResult
	var rules ruleResult
	var plugins pluginResult
	var wait sync.WaitGroup
	wait.Add(3)
	go func() {
		defer wait.Done()
		gatewayData.snapshot, gatewayData.err = service.gateway.Snapshot(ctx)
	}()
	go func() {
		defer wait.Done()
		rules.items, rules.fetchedAt, rules.err = service.guard.RuntimeRules(ctx)
	}()
	go func() {
		defer wait.Done()
		plugins.snapshot, plugins.err = service.guard.ProtectPlugins(ctx)
	}()
	wait.Wait()

	envelope := model.ProtectSnapshotEnvelope{Data: model.ProtectSnapshot{
		GatewayPolicies: []model.ProtectPolicy{}, RuntimeRules: []model.RuntimeRule{},
		Plugins: []model.ProtectPluginPhase{}, Links: service.links,
	}}
	successes := 0
	var failures []error
	if gatewayData.err != nil {
		failures = append(failures, gatewayData.err)
		envelope.Meta.SourceFailures = append(envelope.Meta.SourceFailures, protectFailure(model.SourceAgentGateway, "policies", gatewayData.err))
	} else {
		successes++
		envelope.Data.GatewayPolicies = append(envelope.Data.GatewayPolicies, gatewayData.snapshot.Policies...)
		envelope.Meta.FetchedAt = later(envelope.Meta.FetchedAt, gatewayData.snapshot.FetchedAt)
	}
	if rules.err != nil {
		failures = append(failures, rules.err)
		envelope.Meta.SourceFailures = append(envelope.Meta.SourceFailures, protectFailure(model.SourceAgentGuard, "runtime rules", rules.err))
	} else {
		successes++
		envelope.Data.RuntimeRules = append(envelope.Data.RuntimeRules, rules.items...)
		envelope.Meta.FetchedAt = later(envelope.Meta.FetchedAt, rules.fetchedAt)
	}
	if plugins.err != nil {
		failures = append(failures, plugins.err)
		envelope.Meta.SourceFailures = append(envelope.Meta.SourceFailures, protectFailure(model.SourceAgentGuard, "plugins", plugins.err))
	} else {
		successes++
		envelope.Data.Plugins = append(envelope.Data.Plugins, plugins.snapshot.Items...)
		envelope.Meta.SourceFailures = append(envelope.Meta.SourceFailures, plugins.snapshot.Failures...)
		envelope.Meta.FetchedAt = later(envelope.Meta.FetchedAt, plugins.snapshot.FetchedAt)
	}
	if successes == 0 {
		return model.ProtectSnapshotEnvelope{}, errors.Join(failures...)
	}
	if envelope.Meta.FetchedAt.IsZero() {
		envelope.Meta.FetchedAt = time.Now().UTC()
	}
	envelope.Meta.Partial = len(envelope.Meta.SourceFailures) > 0
	return envelope, nil
}

func (service *Service) Approvals(ctx context.Context, cursor string, limit int) (model.ResourcePageEnvelope[model.Approval], error) {
	items, fetchedAt, err := service.guard.Approvals(ctx)
	if err != nil {
		return model.ResourcePageEnvelope[model.Approval]{}, err
	}
	sortApprovals(items)
	page, err := paginate(items, cursor, limit)
	return model.ResourcePageEnvelope[model.Approval]{
		Data: page, Meta: model.Meta{Source: model.SourceAgentGuard, FetchedAt: fetchedAt},
	}, err
}

func (service *Service) CheckRule(ctx context.Context, source string) (model.RuntimeRuleCheck, error) {
	if !validRuleSource(source) {
		return model.RuntimeRuleCheck{}, ErrInvalidRequest
	}
	result, err := service.guard.CheckRuntimeRule(ctx, source)
	if err != nil {
		return model.RuntimeRuleCheck{}, err
	}
	result.Publishable = result.OK && result.RuleCount == 1
	if !result.Publishable {
		return result, nil
	}
	token, expiresAt := service.issueCheck(source)
	result.CheckToken = token
	result.ExpiresAt = &expiresAt
	return result, nil
}

func (service *Service) PublishRule(ctx context.Context, agentID string, request model.RuntimeRulePublishRequest) (model.ProtectMutationEnvelope, error) {
	if !request.Confirmed || !validNote(request.Note) || !validRuleSource(request.Source) || len(request.CheckToken) > 128 {
		return model.ProtectMutationEnvelope{}, ErrInvalidRequest
	}
	agentUpstreamID, err := service.resolveAgent(ctx, agentID)
	if err != nil {
		return model.ProtectMutationEnvelope{}, err
	}
	if !service.consumeCheck(request.CheckToken, request.Source) {
		return model.ProtectMutationEnvelope{}, ErrRuleCheckRequired
	}
	key := "publish:" + agentID + ":" + digestString(request.Source)
	if !service.beginMutation(key) {
		return model.ProtectMutationEnvelope{}, ErrMutationInFlight
	}
	defer service.endMutation(key)
	ruleID, err := service.guard.PublishRuntimeRule(ctx, agentUpstreamID, request.Source)
	if err != nil {
		return model.ProtectMutationEnvelope{}, err
	}
	return mutationEnvelope("publish-runtime-rule", ruleID, "Runtime rule published"), nil
}

func (service *Service) DeleteRule(ctx context.Context, agentID, ruleID string, request model.ConfirmedActionRequest) (model.ProtectMutationEnvelope, error) {
	if !request.Confirmed || !validNote(request.Note) {
		return model.ProtectMutationEnvelope{}, ErrInvalidRequest
	}
	rules, _, err := service.guard.RuntimeRules(ctx)
	if err != nil {
		return model.ProtectMutationEnvelope{}, err
	}
	var selected *model.RuntimeRule
	for index := range rules {
		if rules[index].ID == ruleID && rules[index].AgentID == agentID && rules[index].UserManaged {
			selected = &rules[index]
			break
		}
	}
	if selected == nil || selected.AgentUpstreamID == "" {
		return model.ProtectMutationEnvelope{}, ErrNotFound
	}
	key := "delete:" + agentID + ":" + ruleID
	if !service.beginMutation(key) {
		return model.ProtectMutationEnvelope{}, ErrMutationInFlight
	}
	defer service.endMutation(key)
	if err := service.guard.DeleteRuntimeRule(ctx, selected.AgentUpstreamID, selected.UpstreamID); err != nil {
		return model.ProtectMutationEnvelope{}, err
	}
	return mutationEnvelope("delete-runtime-rule", ruleID, "Runtime rule deleted"), nil
}

func (service *Service) ResolveApproval(ctx context.Context, ticketID, decision string, request model.ConfirmedActionRequest) (model.ProtectMutationEnvelope, error) {
	if (decision != "approve" && decision != "deny") || !request.Confirmed || !validNote(request.Note) {
		return model.ProtectMutationEnvelope{}, ErrInvalidRequest
	}
	approvals, _, err := service.guard.Approvals(ctx)
	if err != nil {
		return model.ProtectMutationEnvelope{}, err
	}
	upstreamID := ""
	for _, approval := range approvals {
		if approval.ID == ticketID {
			upstreamID = approval.UpstreamID
			break
		}
	}
	if upstreamID == "" {
		return model.ProtectMutationEnvelope{}, ErrNotFound
	}
	key := "approval:" + ticketID
	if !service.beginMutation(key) {
		return model.ProtectMutationEnvelope{}, ErrMutationInFlight
	}
	defer service.endMutation(key)
	if err := service.guard.ResolveApproval(ctx, upstreamID, decision, strings.TrimSpace(request.Note)); err != nil {
		return model.ProtectMutationEnvelope{}, err
	}
	message := "Approval ticket approved"
	if decision == "deny" {
		message = "Approval ticket denied"
	}
	return mutationEnvelope(decision+"-approval", ticketID, message), nil
}

func (service *Service) resolveAgent(ctx context.Context, agentID string) (string, error) {
	if agentID == "" || len(agentID) > 256 {
		return "", ErrInvalidRequest
	}
	snapshot, err := service.guard.TrustSnapshot(ctx)
	if err != nil {
		return "", err
	}
	for _, session := range snapshot.Sessions {
		if session.AgentID == agentID && session.AgentUpstreamID != "" {
			return session.AgentUpstreamID, nil
		}
	}
	for _, resource := range snapshot.Resources {
		if resource.OwnerAgentID == agentID && resource.OwnerAgentUpstreamID != "" {
			return resource.OwnerAgentUpstreamID, nil
		}
	}
	return "", ErrNotFound
}

func (service *Service) issueCheck(source string) (string, time.Time) {
	now := time.Now().UTC()
	expiresAt := now.Add(checkTokenTTL)
	var random [18]byte
	if _, err := rand.Read(random[:]); err != nil {
		copy(random[:], []byte(strconv.FormatInt(now.UnixNano(), 10)))
	}
	token := "check-" + hex.EncodeToString(random[:])
	service.mu.Lock()
	defer service.mu.Unlock()
	service.pruneChecks(now)
	if len(service.checks) >= maxCheckTokens {
		oldestToken := ""
		var oldest time.Time
		for candidate, checked := range service.checks {
			if oldestToken == "" || checked.createdAt.Before(oldest) {
				oldestToken, oldest = candidate, checked.createdAt
			}
		}
		delete(service.checks, oldestToken)
	}
	service.checks[token] = checkedSource{digest: sha256.Sum256([]byte(source)), expiresAt: expiresAt, createdAt: now}
	return token, expiresAt
}

func (service *Service) consumeCheck(token, source string) bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	now := time.Now().UTC()
	service.pruneChecks(now)
	checked, ok := service.checks[token]
	if !ok || checked.digest != sha256.Sum256([]byte(source)) || !checked.expiresAt.After(now) {
		return false
	}
	delete(service.checks, token)
	return true
}

func (service *Service) pruneChecks(now time.Time) {
	for token, checked := range service.checks {
		if !checked.expiresAt.After(now) {
			delete(service.checks, token)
		}
	}
}

func (service *Service) beginMutation(key string) bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	if _, exists := service.active[key]; exists {
		return false
	}
	service.active[key] = struct{}{}
	return true
}

func (service *Service) endMutation(key string) {
	service.mu.Lock()
	delete(service.active, key)
	service.mu.Unlock()
}

func mutationEnvelope(operation, target, message string) model.ProtectMutationEnvelope {
	now := time.Now().UTC()
	return model.ProtectMutationEnvelope{
		Data: model.ProtectMutationReceipt{
			Operation: operation, Status: "succeeded", Source: model.SourceAgentGuard,
			Target: target, CompletedAt: now, Message: message,
		},
		Meta: model.Meta{Source: model.SourceAgentGuard, FetchedAt: now},
	}
}

func validRuleSource(source string) bool {
	return strings.TrimSpace(source) != "" && len(source) <= maxSourceBytes
}

func validNote(note string) bool {
	trimmed := strings.TrimSpace(note)
	return trimmed != "" && len(trimmed) <= maxNoteBytes
}

func digestString(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:8])
}

func protectFailure(source model.Source, capability string, err error) model.SourceFailure {
	code := "UPSTREAM_UNAVAILABLE"
	message := string(source) + " " + capability + " could not be read"
	var gatewayContract *gateway.ContractError
	var guardContract *guard.ContractError
	if errors.As(err, &gatewayContract) || errors.As(err, &guardContract) {
		code = "UPSTREAM_CONTRACT_MISMATCH"
		message = err.Error()
	}
	var upstreamError *upstream.Error
	if errors.As(err, &upstreamError) && upstreamError.Status == 404 {
		code = "UPSTREAM_CAPABILITY_MISSING"
	}
	return model.SourceFailure{Source: source, Code: code, Message: message}
}

func later(current, candidate time.Time) time.Time {
	if candidate.After(current) {
		return candidate
	}
	return current
}

func paginate[T any](items []T, cursor string, limit int) (model.ResourcePage[T], error) {
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 1 || limit > maxLimit {
		return model.ResourcePage[T]{}, ErrInvalidRequest
	}
	offset, err := decodeCursor(cursor)
	if err != nil || offset > len(items) {
		return model.ResourcePage[T]{}, ErrInvalidCursor
	}
	end := min(offset+limit, len(items))
	page := model.ResourcePage[T]{Items: items[offset:end], Total: len(items)}
	if end < len(items) {
		next := base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(end)))
		page.NextCursor = &next
	}
	return page, nil
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

func sortApprovals(items []model.Approval) {
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
}
