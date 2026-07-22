// Package model contains the source-preserving BFF response model.
package model

import "time"

type Source string

const (
	SourceAgentGateway Source = "agentgateway"
	SourceAgentGuard   Source = "agentguard"
)

type HealthStatus string

const (
	HealthHealthy  HealthStatus = "healthy"
	HealthDegraded HealthStatus = "degraded"
	HealthDown     HealthStatus = "down"
	HealthUnknown  HealthStatus = "unknown"
)

type CapabilityStatus string

const (
	CapabilitySupported   CapabilityStatus = "supported"
	CapabilityPartial     CapabilityStatus = "partial"
	CapabilityLinkOut     CapabilityStatus = "link-out"
	CapabilityUnavailable CapabilityStatus = "unavailable"
)

type SourceHealth struct {
	Source    Source       `json:"source"`
	Label     string       `json:"label"`
	Status    HealthStatus `json:"status"`
	Version   string       `json:"version,omitempty"`
	LatencyMS *int64       `json:"latencyMs"`
	CheckedAt time.Time    `json:"checkedAt"`
	Message   string       `json:"message,omitempty"`
}

type Capability struct {
	ID        string           `json:"id"`
	Source    Source           `json:"source"`
	Status    CapabilityStatus `json:"status"`
	CheckedAt time.Time        `json:"checkedAt"`
	Reason    string           `json:"reason,omitempty"`
}

type SourceFailure struct {
	Source  Source `json:"source"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Meta struct {
	Source         Source          `json:"source,omitempty"`
	SourceVersion  string          `json:"sourceVersion,omitempty"`
	FetchedAt      time.Time       `json:"fetchedAt"`
	Stale          bool            `json:"stale"`
	Partial        bool            `json:"partial"`
	SourceFailures []SourceFailure `json:"sourceFailures,omitempty"`
}

type HealthEnvelope struct {
	Data []SourceHealth `json:"data"`
	Meta Meta           `json:"meta"`
}

type CapabilitiesEnvelope struct {
	Data []Capability `json:"data"`
	Meta Meta         `json:"meta"`
}

type SetupStep struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Complete bool   `json:"complete"`
}

type Setup struct {
	Complete bool        `json:"complete"`
	Steps    []SetupStep `json:"steps"`
}

// OverviewData intentionally leaves business collections empty in Phase 2.
// Later phases populate them only from verified upstream contracts.
type OverviewData struct {
	Environment string         `json:"environment"`
	Mode        string         `json:"mode"`
	Health      []SourceHealth `json:"health"`
	Metrics     []any          `json:"metrics"`
	Trend       []any          `json:"trend"`
	Events      []UnifiedEvent `json:"events"`
	Setup       Setup          `json:"setup"`
}

type OverviewEnvelope struct {
	Data OverviewData `json:"data"`
	Meta Meta         `json:"meta"`
}

type RawRef struct {
	Source Source `json:"source"`
	ID     string `json:"id"`
}

type UnifiedEvent struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Source    Source    `json:"source"`
	Kind      string    `json:"kind"`
	Severity  string    `json:"severity"`
	Summary   string    `json:"summary"`
	RawRef    RawRef    `json:"rawRef"`
}

type APIError struct {
	Code      string  `json:"code"`
	Message   string  `json:"message"`
	Source    *Source `json:"source,omitempty"`
	RequestID string  `json:"requestId"`
	Retryable bool    `json:"retryable"`
}

type ErrorEnvelope struct {
	Error APIError `json:"error"`
}
