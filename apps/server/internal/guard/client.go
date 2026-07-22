// Package guard adapts verified AgentGuard management contracts.
package guard

import (
	"context"
	"net/http"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/upstream"
)

type Client struct {
	upstream       *upstream.Client
	operations     *upstream.Client
	releaseVersion string
}

func New(baseURL, apiKey string, httpClient *http.Client, retryMax int) (*Client, error) {
	return NewWithRelease(baseURL, apiKey, "", httpClient, retryMax)
}

func NewWithRelease(baseURL, apiKey, releaseVersion string, httpClient *http.Client, retryMax int) (*Client, error) {
	return NewWithOperationClient(baseURL, apiKey, releaseVersion, httpClient, httpClient, retryMax)
}

func NewWithOperationClient(baseURL, apiKey, releaseVersion string, httpClient, operationHTTPClient *http.Client, retryMax int) (*Client, error) {
	client, err := upstream.New(model.SourceAgentGuard, baseURL, "X-Api-Key", apiKey, httpClient, retryMax)
	if err != nil {
		return nil, err
	}
	operations, err := upstream.New(model.SourceAgentGuard, baseURL, "X-Api-Key", apiKey, operationHTTPClient, 0)
	if err != nil {
		return nil, err
	}
	return &Client{upstream: client, operations: operations, releaseVersion: releaseVersion}, nil
}

type healthResponse struct {
	OK      bool   `json:"ok"`
	Status  string `json:"status"`
	Version string `json:"version"`
	Service string `json:"service"`
}

func (client *Client) Health(ctx context.Context) model.SourceHealth {
	checkedAt := time.Now().UTC()
	var response healthResponse
	duration, err := client.upstream.GetJSON(ctx, "/v1/backend/health", &response)
	latency := duration.Milliseconds()
	health := model.SourceHealth{
		Source: model.SourceAgentGuard, Label: "AgentGuard", Status: model.HealthHealthy,
		Version: response.Version, LatencyMS: &latency, CheckedAt: checkedAt,
	}
	if client.releaseVersion != "" && response.Version != "" && err == nil {
		health.Version = client.releaseVersion + " · API " + response.Version
	}
	if err != nil {
		health.Status = model.HealthDown
		health.Version = ""
		health.Message = err.Error()
		return health
	}
	if !response.OK || response.Status != "ok" || response.Service != "agentguard-server" {
		health.Status = model.HealthDegraded
		health.Message = "health contract reported a degraded state"
	}
	return health
}

func (client *Client) Capabilities(ctx context.Context) []model.Capability {
	checkedAt := time.Now().UTC()
	probes := []struct {
		id   string
		path string
	}{
		{"guard.health", "/v1/backend/health"},
		{"guard.sessions", "/v1/backend/sessions"},
		{"guard.tools", "/v1/backend/tools"},
		{"guard.skills", "/v1/backend/skills"},
		{"guard.mcps", "/v1/backend/mcps"},
		{"guard.rules", "/v1/backend/rules"},
		{"guard.traffic", "/v1/backend/traffic"},
		{"guard.audit", "/v1/backend/audit/recent"},
		{"guard.approvals", "/v1/backend/approvals"},
		{"guard.auditors", "/v1/backend/auditors"},
	}
	capabilities := make([]model.Capability, 0, len(probes))
	for _, probe := range probes {
		err := client.upstream.ProbeJSON(ctx, probe.path)
		capabilities = append(capabilities, capability(probe.id, err, checkedAt, probe.path))
	}
	return capabilities
}

func capability(id string, err error, checkedAt time.Time, path string) model.Capability {
	if err == nil {
		return model.Capability{ID: id, Source: model.SourceAgentGuard, Status: model.CapabilitySupported, CheckedAt: checkedAt, Reason: "live " + path + " probe succeeded"}
	}
	return model.Capability{ID: id, Source: model.SourceAgentGuard, Status: model.CapabilityUnavailable, CheckedAt: checkedAt, Reason: "live probe failed"}
}
