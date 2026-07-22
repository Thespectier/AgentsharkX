// Package aggregate combines independent upstream results without hiding partial failure.
package aggregate

import (
	"context"
	"sync"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

type Source interface {
	Health(context.Context) model.SourceHealth
	Capabilities(context.Context) []model.Capability
}

type OperationalSource interface {
	OperationalSnapshot() model.OperationalSnapshot
}

type Service struct {
	mu          sync.RWMutex
	environment string
	gateway     Source
	guard       Source
	health      []model.SourceHealth
	operational OperationalSource
}

func (service *Service) SetOperational(source OperationalSource) {
	service.mu.Lock()
	service.operational = source
	service.mu.Unlock()
}

func New(environment string, gateway, guard Source) *Service {
	return &Service{
		environment: environment,
		gateway:     gateway,
		guard:       guard,
		health: []model.SourceHealth{
			{Source: model.SourceAgentGateway, Label: "agentgateway", Status: model.HealthUnknown, CheckedAt: time.Now().UTC()},
			{Source: model.SourceAgentGuard, Label: "AgentGuard", Status: model.HealthUnknown, CheckedAt: time.Now().UTC()},
		},
	}
}

func (service *Service) Refresh(ctx context.Context) []model.SourceHealth {
	var wait sync.WaitGroup
	health := make([]model.SourceHealth, 2)
	wait.Add(2)
	go func() {
		defer wait.Done()
		health[0] = service.gateway.Health(ctx)
	}()
	go func() {
		defer wait.Done()
		health[1] = service.guard.Health(ctx)
	}()
	wait.Wait()
	service.mu.Lock()
	service.health = cloneHealth(health)
	service.mu.Unlock()
	return health
}

func (service *Service) Snapshot() []model.SourceHealth {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return cloneHealth(service.health)
}

func (service *Service) Health() model.HealthEnvelope {
	health := service.Snapshot()
	return model.HealthEnvelope{Data: health, Meta: metaFor(health)}
}

func (service *Service) Capabilities(ctx context.Context) model.CapabilitiesEnvelope {
	var wait sync.WaitGroup
	var gatewayCapabilities, guardCapabilities []model.Capability
	wait.Add(2)
	go func() {
		defer wait.Done()
		gatewayCapabilities = service.gateway.Capabilities(ctx)
	}()
	go func() {
		defer wait.Done()
		guardCapabilities = service.guard.Capabilities(ctx)
	}()
	wait.Wait()
	capabilities := append(gatewayCapabilities, guardCapabilities...)
	return model.CapabilitiesEnvelope{Data: capabilities, Meta: metaFor(service.Snapshot())}
}

func (service *Service) Diagnostics() model.DiagnosticsEnvelope {
	health := service.Snapshot()
	status := model.HealthHealthy
	issues := make([]model.DiagnosticIssue, 0, len(health))
	down := 0
	for _, source := range health {
		if source.Status == model.HealthHealthy {
			continue
		}
		if source.Status == model.HealthDown || source.Status == model.HealthUnknown {
			down++
		}
		issue := model.DiagnosticIssue{
			Source: source.Source, Status: source.Status,
			Summary: "The upstream management API is not ready.",
			Checks: []string{
				"Confirm the configured base URL reaches the upstream management port from the AgentsharkX container.",
				"Inspect the upstream container health and logs, then retry the probe.",
			},
			DocumentationPath: "https://github.com/Thespectier/AgentsharkX/blob/main/docs/troubleshooting.md#upstream-connectivity",
		}
		switch source.Source {
		case model.SourceAgentGateway:
			issue.Summary = "agentgateway management probes are unavailable. Connect and gateway audit data may be incomplete."
			issue.Checks = append(issue.Checks,
				"Verify AGENTGATEWAY_BASE_URL targets the admin listener, not an agent traffic listener.",
				"Run the read-only upstream smoke check after agentgateway reports ready.",
			)
		case model.SourceAgentGuard:
			issue.Summary = "AgentGuard management probes are unavailable. Trust, runtime protection, and approvals may be incomplete."
			issue.Checks = append(issue.Checks,
				"Verify AGENTGUARD_BASE_URL and that AGENTGUARD_ADMIN_TOKEN matches the AgentGuard API key.",
				"Confirm GET /v1/backend/health succeeds from the AgentsharkX network.",
			)
		}
		issues = append(issues, issue)
	}
	if len(issues) > 0 {
		status = model.HealthDegraded
	}
	if len(health) > 0 && down == len(health) {
		status = model.HealthDown
	}
	return model.DiagnosticsEnvelope{
		Data: model.DiagnosticsData{Status: status, Issues: issues},
		Meta: metaFor(health),
	}
}

func (service *Service) Overview() model.OverviewEnvelope {
	health := service.Snapshot()
	service.mu.RLock()
	operational := service.operational
	service.mu.RUnlock()
	allHealthy := len(health) == 2
	steps := make([]model.SetupStep, 0, len(health))
	for _, source := range health {
		complete := source.Status == model.HealthHealthy
		allHealthy = allHealthy && complete
		steps = append(steps, model.SetupStep{ID: string(source.Source), Label: "Connect " + source.Label, Complete: complete})
	}
	envelope := model.OverviewEnvelope{
		Data: model.OverviewData{
			Environment: service.environment,
			Mode:        "health-only",
			Health:      health,
			Metrics:     []model.Metric{},
			Trend:       []model.TrendPoint{},
			Events:      []model.UnifiedEvent{},
			Setup:       model.Setup{Complete: allHealthy, Steps: steps},
		},
		Meta: metaFor(health),
	}
	if operational != nil {
		snapshot := operational.OperationalSnapshot()
		envelope.Data.Mode = "operational"
		envelope.Data.Metrics = snapshot.Metrics
		envelope.Data.Trend = snapshot.Trend
		envelope.Data.Events = snapshot.Events
		envelope.Meta.Partial = envelope.Meta.Partial || snapshot.Meta.Partial
		envelope.Meta.SourceFailures = append(envelope.Meta.SourceFailures, snapshot.Meta.SourceFailures...)
		if snapshot.Meta.FetchedAt.After(envelope.Meta.FetchedAt) {
			envelope.Meta.FetchedAt = snapshot.Meta.FetchedAt
		}
	}
	return envelope
}

func metaFor(health []model.SourceHealth) model.Meta {
	meta := model.Meta{FetchedAt: time.Now().UTC(), SourceFailures: []model.SourceFailure{}}
	for _, source := range health {
		if source.Status == model.HealthHealthy {
			continue
		}
		meta.Partial = true
		code := "UPSTREAM_DEGRADED"
		if source.Status == model.HealthDown || source.Status == model.HealthUnknown {
			code = "UPSTREAM_UNAVAILABLE"
		}
		message := source.Message
		if message == "" {
			message = "source is not healthy"
		}
		meta.SourceFailures = append(meta.SourceFailures, model.SourceFailure{Source: source.Source, Code: code, Message: message})
	}
	return meta
}

func cloneHealth(input []model.SourceHealth) []model.SourceHealth {
	return append([]model.SourceHealth(nil), input...)
}
