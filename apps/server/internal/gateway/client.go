// Package gateway adapts verified agentgateway management contracts.
package gateway

import (
	"context"
	"net/http"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/upstream"
)

type Client struct{ upstream *upstream.Client }

func New(baseURL string, httpClient *http.Client, retryMax int) (*Client, error) {
	client, err := upstream.New(model.SourceAgentGateway, baseURL, "", "", httpClient, retryMax)
	if err != nil {
		return nil, err
	}
	return &Client{upstream: client}, nil
}

type runtimeResponse struct {
	Build struct {
		Version string `json:"version"`
	} `json:"build"`
}

func (client *Client) Health(ctx context.Context) model.SourceHealth {
	checkedAt := time.Now().UTC()
	var runtime runtimeResponse
	duration, err := client.upstream.GetJSON(ctx, "/api/runtime", &runtime)
	latency := duration.Milliseconds()
	health := model.SourceHealth{
		Source: model.SourceAgentGateway, Label: "agentgateway", Status: model.HealthHealthy,
		Version: runtime.Build.Version, LatencyMS: &latency, CheckedAt: checkedAt,
	}
	if err != nil {
		health.Status = model.HealthDown
		health.Version = ""
		health.Message = err.Error()
	}
	return health
}

func (client *Client) Capabilities(ctx context.Context) []model.Capability {
	checkedAt := time.Now().UTC()
	runtimeErr := client.upstream.ProbeJSON(ctx, "/api/runtime")
	configErr := client.upstream.ProbeJSON(ctx, "/api/config")
	dumpErr := client.upstream.ProbeJSON(ctx, "/config_dump")
	costErr := client.upstream.ProbeJSON(ctx, "/api/costs/models")
	requestLogs, requestLogsErr := client.Traffic(ctx, 1)

	configurationStatus := model.CapabilitySupported
	configurationReason := "live probes confirmed /api/config and /config_dump"
	if configErr != nil && dumpErr != nil {
		configurationStatus = model.CapabilityUnavailable
		configurationReason = "live configuration probes failed"
	} else if configErr != nil || dumpErr != nil {
		configurationStatus = model.CapabilityPartial
		configurationReason = "only one live configuration probe succeeded"
	}

	requestLogsStatus := model.CapabilitySupported
	requestLogsReason := "live redacted /api/logs/search probe succeeded"
	if requestLogsErr != nil {
		requestLogsStatus = model.CapabilityUnavailable
		requestLogsReason = "live request-log probe failed"
	} else if requestLogs.Status != "available" {
		requestLogsStatus = model.CapabilityUnavailable
		requestLogsReason = requestLogs.Reason
	}

	return []model.Capability{
		capability("gateway.runtime", model.SourceAgentGateway, runtimeErr, checkedAt, "live /api/runtime probe succeeded"),
		{ID: "gateway.configuration", Source: model.SourceAgentGateway, Status: configurationStatus, CheckedAt: checkedAt, Reason: configurationReason},
		capability("gateway.cost-catalog", model.SourceAgentGateway, costErr, checkedAt, "live /api/costs/models probe succeeded"),
		{ID: "gateway.request-logs", Source: model.SourceAgentGateway, Status: requestLogsStatus, CheckedAt: checkedAt, Reason: requestLogsReason},
		{ID: "gateway.admin-auth", Source: model.SourceAgentGateway, Status: model.CapabilityUnavailable, CheckedAt: checkedAt, Reason: "pinned upstream does not expose native admin authentication"},
	}
}

func capability(id string, source model.Source, err error, checkedAt time.Time, successReason string) model.Capability {
	if err == nil {
		return model.Capability{ID: id, Source: source, Status: model.CapabilitySupported, CheckedAt: checkedAt, Reason: successReason}
	}
	return model.Capability{ID: id, Source: source, Status: model.CapabilityUnavailable, CheckedAt: checkedAt, Reason: "live probe failed"}
}
