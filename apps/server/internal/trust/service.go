// Package trust implements source-preserving AgentGuard identity and resource workflows.
package trust

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/guard"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/upstream"
)

const (
	defaultLimit = 25
	maxLimit     = 100
	maxScanJobs  = 100
)

var (
	ErrInvalidCursor  = errors.New("invalid pagination cursor")
	ErrNotFound       = errors.New("trust resource not found")
	ErrInvalidRequest = errors.New("invalid trust request")
	ErrScanCapacity   = errors.New("scan job capacity reached")
)

type Guard interface {
	TrustSnapshot(context.Context) (model.TrustSnapshot, error)
	UpdateToolLabels(context.Context, string, string, model.TrustLabelUpdate) (model.TrustResource, error)
	DetectSkills(context.Context, string, []string, bool) ([]model.TrustDetection, []string, error)
	DetectMCPs(context.Context, string, []string) ([]model.TrustDetection, []string, error)
}

type Service struct {
	guard       Guard
	root        context.Context
	scanTimeout time.Duration
	mu          sync.RWMutex
	jobs        map[string]model.TrustScanJob
	jobOrder    []string
}

func New(root context.Context, guardClient Guard, scanTimeout time.Duration) *Service {
	if root == nil {
		root = context.Background()
	}
	if scanTimeout <= 0 {
		scanTimeout = 90 * time.Second
	}
	return &Service{guard: guardClient, root: root, scanTimeout: scanTimeout, jobs: make(map[string]model.TrustScanJob)}
}

func (service *Service) Agents(ctx context.Context, query, cursor string, limit int) (model.ResourcePageEnvelope[model.TrustAgent], error) {
	snapshot, err := service.guard.TrustSnapshot(ctx)
	if err != nil {
		return model.ResourcePageEnvelope[model.TrustAgent]{}, err
	}
	agents := filterAgents(aggregateAgents(snapshot), query)
	page, err := paginate(agents, cursor, limit)
	return model.ResourcePageEnvelope[model.TrustAgent]{Data: page, Meta: trustMeta(snapshot)}, err
}

func (service *Service) Agent(ctx context.Context, id string) (model.ResourceEnvelope[model.TrustAgentWorkspace], error) {
	snapshot, err := service.guard.TrustSnapshot(ctx)
	if err != nil {
		return model.ResourceEnvelope[model.TrustAgentWorkspace]{}, err
	}
	for _, agent := range aggregateAgents(snapshot) {
		if agent.ID != id {
			continue
		}
		workspace := model.TrustAgentWorkspace{Agent: agent, Sessions: []model.TrustSession{}, Resources: []model.TrustResource{}}
		for _, session := range snapshot.Sessions {
			if session.AgentID == id {
				workspace.Sessions = append(workspace.Sessions, session)
			}
		}
		for _, resource := range snapshot.Resources {
			if resource.OwnerAgentID == id {
				workspace.Resources = append(workspace.Resources, resource)
			}
		}
		return model.ResourceEnvelope[model.TrustAgentWorkspace]{Data: workspace, Meta: trustMeta(snapshot)}, nil
	}
	return model.ResourceEnvelope[model.TrustAgentWorkspace]{}, ErrNotFound
}

func (service *Service) Resources(ctx context.Context, query, resourceType, agentID, cursor string, limit int) (model.ResourcePageEnvelope[model.TrustResource], error) {
	if resourceType != "" && resourceType != "tool" && resourceType != "skill" && resourceType != "mcp" {
		return model.ResourcePageEnvelope[model.TrustResource]{}, ErrInvalidRequest
	}
	snapshot, err := service.guard.TrustSnapshot(ctx)
	if err != nil {
		return model.ResourcePageEnvelope[model.TrustResource]{}, err
	}
	normalized := strings.ToLower(strings.TrimSpace(query))
	items := make([]model.TrustResource, 0, len(snapshot.Resources))
	for _, item := range snapshot.Resources {
		if resourceType != "" && item.Type != resourceType {
			continue
		}
		if agentID != "" && item.OwnerAgentID != agentID {
			continue
		}
		text := strings.ToLower(item.Name + " " + item.UpstreamID + " " + item.OwnerAgentUpstreamID + " " + item.Framework + " " + item.Transport)
		if normalized == "" || strings.Contains(text, normalized) {
			items = append(items, item)
		}
	}
	page, err := paginate(items, cursor, limit)
	return model.ResourcePageEnvelope[model.TrustResource]{Data: page, Meta: trustMeta(snapshot)}, err
}

func (service *Service) UpdateToolLabels(ctx context.Context, agentID, resourceID string, update model.TrustLabelUpdate) (model.ResourceEnvelope[model.TrustResource], error) {
	if err := validateLabels(update); err != nil {
		return model.ResourceEnvelope[model.TrustResource]{}, err
	}
	snapshot, err := service.guard.TrustSnapshot(ctx)
	if err != nil {
		return model.ResourceEnvelope[model.TrustResource]{}, err
	}
	for _, resource := range snapshot.Resources {
		if resource.ID != resourceID || resource.Type != "tool" || resource.OwnerAgentID != agentID {
			continue
		}
		updated, err := service.guard.UpdateToolLabels(ctx, resource.OwnerAgentUpstreamID, resource.UpstreamID, update)
		if err != nil {
			return model.ResourceEnvelope[model.TrustResource]{}, err
		}
		if updated.OwnerAgentUpstreamID != resource.OwnerAgentUpstreamID || updated.UpstreamID != resource.UpstreamID {
			return model.ResourceEnvelope[model.TrustResource]{}, &guard.ContractError{
				Field:   "/v1/backend/agents/{agent_id}/tools/{tool_name}/labels/tool",
				Problem: "updated tool identity did not match the request",
			}
		}
		return model.ResourceEnvelope[model.TrustResource]{Data: updated, Meta: trustMeta(snapshot)}, nil
	}
	return model.ResourceEnvelope[model.TrustResource]{}, ErrNotFound
}

func (service *Service) StartScan(ctx context.Context, agentID, resourceType string, request model.TrustDetectionRequest) (model.ResourceEnvelope[model.TrustScanJob], error) {
	if resourceType != "skill" && resourceType != "mcp" {
		return model.ResourceEnvelope[model.TrustScanJob]{}, ErrInvalidRequest
	}
	if resourceType == "mcp" && request.UseLLM {
		return model.ResourceEnvelope[model.TrustScanJob]{}, ErrInvalidRequest
	}
	ids, err := uniqueIDs(request.ResourceIDs)
	if err != nil {
		return model.ResourceEnvelope[model.TrustScanJob]{}, err
	}
	snapshot, err := service.guard.TrustSnapshot(ctx)
	if err != nil {
		return model.ResourceEnvelope[model.TrustScanJob]{}, err
	}
	requested := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		requested[id] = struct{}{}
	}
	upstreamIDs := make([]string, 0, len(ids))
	resourceIDs := make([]string, 0, len(ids))
	agentUpstreamID := ""
	for _, resource := range snapshot.Resources {
		if _, ok := requested[resource.ID]; !ok || resource.Type != resourceType || resource.OwnerAgentID != agentID {
			continue
		}
		resourceIDs = append(resourceIDs, resource.ID)
		upstreamIDs = append(upstreamIDs, resource.UpstreamID)
		agentUpstreamID = resource.OwnerAgentUpstreamID
	}
	if len(resourceIDs) != len(ids) || agentUpstreamID == "" {
		return model.ResourceEnvelope[model.TrustScanJob]{}, ErrNotFound
	}

	job, err := service.newJob(agentID, agentUpstreamID, resourceType, resourceIDs)
	if err != nil {
		return model.ResourceEnvelope[model.TrustScanJob]{}, err
	}
	go service.runJob(job.ID, upstreamIDs, request.UseLLM)
	return model.ResourceEnvelope[model.TrustScanJob]{Data: job, Meta: jobMeta(job)}, nil
}

func (service *Service) ScanJob(id string) (model.ResourceEnvelope[model.TrustScanJob], error) {
	service.mu.RLock()
	job, ok := service.jobs[id]
	service.mu.RUnlock()
	if !ok {
		return model.ResourceEnvelope[model.TrustScanJob]{}, ErrNotFound
	}
	job = cloneJob(job)
	return model.ResourceEnvelope[model.TrustScanJob]{Data: job, Meta: jobMeta(job)}, nil
}

func (service *Service) ScanJobs(cursor string, limit int) (model.ResourcePageEnvelope[model.TrustScanJob], error) {
	service.mu.RLock()
	items := make([]model.TrustScanJob, 0, len(service.jobOrder))
	for index := len(service.jobOrder) - 1; index >= 0; index-- {
		items = append(items, cloneJob(service.jobs[service.jobOrder[index]]))
	}
	service.mu.RUnlock()
	page, err := paginate(items, cursor, limit)
	fetchedAt := time.Now().UTC()
	if len(items) > 0 {
		fetchedAt = items[0].UpdatedAt
	}
	return model.ResourcePageEnvelope[model.TrustScanJob]{Data: page, Meta: model.Meta{Source: model.SourceAgentGuard, FetchedAt: fetchedAt}}, err
}

func (service *Service) newJob(agentID, upstreamAgentID, resourceType string, resourceIDs []string) (model.TrustScanJob, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.pruneCompletedJobs()
	if len(service.jobs) >= maxScanJobs {
		return model.TrustScanJob{}, ErrScanCapacity
	}
	now := time.Now().UTC()
	job := model.TrustScanJob{
		ID: newJobID(), Source: model.SourceAgentGuard, AgentID: agentID, AgentUpstreamID: upstreamAgentID,
		ResourceType: resourceType, ResourceIDs: append([]string{}, resourceIDs...), Status: "queued",
		CreatedAt: now, UpdatedAt: now, Results: []model.TrustDetection{}, Warnings: []string{},
	}
	service.jobs[job.ID] = job
	service.jobOrder = append(service.jobOrder, job.ID)
	return cloneJob(job), nil
}

func (service *Service) runJob(jobID string, upstreamIDs []string, useLLM bool) {
	now := time.Now().UTC()
	service.updateJob(jobID, func(job *model.TrustScanJob) {
		job.Status = "running"
		job.StartedAt = &now
		job.UpdatedAt = now
	})
	service.mu.RLock()
	job := service.jobs[jobID]
	service.mu.RUnlock()
	ctx, cancel := context.WithTimeout(service.root, service.scanTimeout)
	defer cancel()

	var results []model.TrustDetection
	var warnings []string
	var err error
	if job.ResourceType == "skill" {
		results, warnings, err = service.guard.DetectSkills(ctx, job.AgentUpstreamID, upstreamIDs, useLLM)
	} else {
		results, warnings, err = service.guard.DetectMCPs(ctx, job.AgentUpstreamID, upstreamIDs)
	}
	completedAt := time.Now().UTC()
	service.updateJob(jobID, func(job *model.TrustScanJob) {
		job.CompletedAt = &completedAt
		job.UpdatedAt = completedAt
		job.Results = append([]model.TrustDetection{}, results...)
		job.Warnings = append([]string{}, warnings...)
		if err == nil {
			job.Status = "succeeded"
			return
		}
		job.Status = "failed"
		job.Error = scanError(err)
	})
}

func (service *Service) updateJob(id string, update func(*model.TrustScanJob)) {
	service.mu.Lock()
	defer service.mu.Unlock()
	job, ok := service.jobs[id]
	if !ok {
		return
	}
	update(&job)
	service.jobs[id] = job
}

func (service *Service) pruneCompletedJobs() {
	if len(service.jobs) < maxScanJobs {
		return
	}
	remaining := service.jobOrder[:0]
	for _, id := range service.jobOrder {
		job := service.jobs[id]
		if len(service.jobs) >= maxScanJobs && (job.Status == "succeeded" || job.Status == "failed") {
			delete(service.jobs, id)
			continue
		}
		remaining = append(remaining, id)
	}
	service.jobOrder = remaining
}

func aggregateAgents(snapshot model.TrustSnapshot) []model.TrustAgent {
	type accumulator struct {
		id         string
		upstreamID string
		frameworks map[string]struct{}
		sessions   int
		tools      int
		skills     int
		mcps       int
		lastActive *time.Time
	}
	values := map[string]*accumulator{}
	get := func(id, upstreamID string) *accumulator {
		if upstreamID == "" {
			return nil
		}
		if current := values[upstreamID]; current != nil {
			return current
		}
		current := &accumulator{id: id, upstreamID: upstreamID, frameworks: map[string]struct{}{}}
		values[upstreamID] = current
		return current
	}
	for _, session := range snapshot.Sessions {
		current := get(session.AgentID, session.AgentUpstreamID)
		if current == nil {
			continue
		}
		current.sessions++
		if session.LastSeen != nil && (current.lastActive == nil || session.LastSeen.After(*current.lastActive)) {
			value := *session.LastSeen
			current.lastActive = &value
		}
	}
	for _, resource := range snapshot.Resources {
		current := get(resource.OwnerAgentID, resource.OwnerAgentUpstreamID)
		if current == nil {
			continue
		}
		if resource.Framework != "" {
			current.frameworks[resource.Framework] = struct{}{}
		}
		switch resource.Type {
		case "tool":
			current.tools++
		case "skill":
			current.skills++
		case "mcp":
			current.mcps++
		}
	}
	agents := make([]model.TrustAgent, 0, len(values))
	for _, value := range values {
		agent := model.TrustAgent{
			TrustResourceBase: model.TrustResourceBase{
				ID: value.id, UpstreamID: value.upstreamID, Source: model.SourceAgentGuard, FetchedAt: snapshot.FetchedAt,
				RawRef: model.RawRef{Source: model.SourceAgentGuard, ID: "agent_id:" + value.upstreamID},
			},
			Name: value.upstreamID, Status: "unknown", Sessions: value.sessions, Tools: value.tools,
			Skills: value.skills, MCPs: value.mcps, LastActive: value.lastActive,
		}
		agent.Framework = singleValue(value.frameworks)
		agents = append(agents, agent)
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].UpstreamID < agents[j].UpstreamID })
	return agents
}

func singleValue(values map[string]struct{}) *string {
	if len(values) != 1 {
		return nil
	}
	for value := range values {
		result := value
		return &result
	}
	return nil
}

func filterAgents(items []model.TrustAgent, query string) []model.TrustAgent {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return items
	}
	filtered := make([]model.TrustAgent, 0, len(items))
	for _, item := range items {
		text := strings.ToLower(item.UpstreamID)
		if item.Framework != nil {
			text += " " + strings.ToLower(*item.Framework)
		}
		if item.Principal != nil {
			text += " " + strings.ToLower(*item.Principal)
		}
		if strings.Contains(text, normalized) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func trustMeta(snapshot model.TrustSnapshot) model.Meta {
	return model.Meta{
		Source: model.SourceAgentGuard, FetchedAt: snapshot.FetchedAt, Partial: len(snapshot.Failures) > 0,
		SourceFailures: append([]model.SourceFailure{}, snapshot.Failures...),
	}
}

func jobMeta(job model.TrustScanJob) model.Meta {
	return model.Meta{Source: model.SourceAgentGuard, FetchedAt: job.UpdatedAt, Partial: job.Status == "failed"}
}

func validateLabels(update model.TrustLabelUpdate) error {
	if update.Boundary == nil && update.Sensitivity == nil && update.Integrity == nil && update.Tags == nil {
		return ErrInvalidRequest
	}
	for _, value := range []*string{update.Boundary, update.Sensitivity, update.Integrity} {
		if value != nil && (strings.TrimSpace(*value) == "" || len(*value) > 64) {
			return ErrInvalidRequest
		}
	}
	if update.Tags != nil {
		if len(*update.Tags) > 20 {
			return ErrInvalidRequest
		}
		for _, tag := range *update.Tags {
			if strings.TrimSpace(tag) == "" || len(tag) > 64 {
				return ErrInvalidRequest
			}
		}
	}
	return nil
}

func uniqueIDs(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > 25 {
		return nil, ErrInvalidRequest
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 256 {
			return nil, ErrInvalidRequest
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
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

func scanError(err error) *model.TrustScanError {
	result := &model.TrustScanError{Code: "UPSTREAM_UNAVAILABLE", Message: "AgentGuard detection could not be completed", Retryable: true}
	if errors.Is(err, context.DeadlineExceeded) {
		result.Code = "DETECTION_TIMEOUT"
		result.Message = "AgentGuard detection exceeded the configured time limit"
		return result
	}
	var contractError *guard.ContractError
	if errors.As(err, &contractError) {
		result.Code = "UPSTREAM_CONTRACT_MISMATCH"
		result.Message = contractError.Error()
		result.Retryable = false
		return result
	}
	var upstreamError *upstream.Error
	if errors.As(err, &upstreamError) {
		result.Retryable = upstreamError.Retryable || upstreamError.Status >= 500 || upstreamError.Status == 0
		switch upstreamError.Status {
		case 404:
			result.Code = "RESOURCE_NOT_FOUND"
			result.Message = "The requested AgentGuard resource is no longer available"
			result.Retryable = false
		case 400, 422:
			result.Code = "DETECTION_REJECTED"
			result.Message = "AgentGuard rejected the detection request"
			result.Retryable = false
		}
	}
	return result
}

func cloneJob(job model.TrustScanJob) model.TrustScanJob {
	job.ResourceIDs = append([]string{}, job.ResourceIDs...)
	job.Results = append([]model.TrustDetection{}, job.Results...)
	job.Warnings = append([]string{}, job.Warnings...)
	if job.Error != nil {
		copy := *job.Error
		job.Error = &copy
	}
	return job
}

func newJobID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("scan-%d", time.Now().UnixNano())
	}
	return "scan-" + hex.EncodeToString(value[:])
}
