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

// OverviewData remains health-only through Phase 3. Later phases populate its
// business collections only from verified upstream contracts.
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

type ConnectResource struct {
	ID         string    `json:"id"`
	UpstreamID string    `json:"upstreamId,omitempty"`
	Source     Source    `json:"source"`
	FetchedAt  time.Time `json:"fetchedAt"`
	RawRef     RawRef    `json:"rawRef"`
}

type GatewayProvider struct {
	ConnectResource
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	ModelCount int    `json:"modelCount"`
}

type GatewayModel struct {
	ConnectResource
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Provider    string   `json:"provider,omitempty"`
	TargetModel string   `json:"targetModel,omitempty"`
	Routing     string   `json:"routing,omitempty"`
	Targets     []string `json:"targets,omitempty"`
}

type GatewayMCPServer struct {
	ConnectResource
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Scope     string `json:"scope"`
}

type GatewayRoute struct {
	ConnectResource
	Name                    string   `json:"name"`
	Listener                string   `json:"listener"`
	Protocol                string   `json:"protocol"`
	Port                    int      `json:"port"`
	Hostnames               []string `json:"hostnames"`
	Path                    string   `json:"path,omitempty"`
	Targets                 []string `json:"targets"`
	BackendCount            int      `json:"backendCount"`
	UnavailableBackendCount int      `json:"unavailableBackendCount"`
}

type GatewaySnapshot struct {
	Providers []GatewayProvider
	Models    []GatewayModel
	MCP       []GatewayMCPServer
	Routes    []GatewayRoute
	Listeners int
	Backends  int
	FetchedAt time.Time
}

type AnalyticsBucket struct {
	Start       time.Time `json:"start"`
	Requests    int64     `json:"requests"`
	TotalTokens int64     `json:"totalTokens"`
	Cost        float64   `json:"cost"`
}

type GatewayAnalytics struct {
	Status        string            `json:"status"`
	Reason        string            `json:"reason,omitempty"`
	Requests      *int64            `json:"requests"`
	TotalTokens   *int64            `json:"totalTokens"`
	Cost          *float64          `json:"cost"`
	BucketSeconds *int64            `json:"bucketSeconds"`
	Buckets       []AnalyticsBucket `json:"buckets"`
}

type ConnectCount struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Value  *int   `json:"value"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type ConsoleLinks struct {
	Console       string `json:"console,omitempty"`
	RawConfig     string `json:"rawConfig,omitempty"`
	CEL           string `json:"cel,omitempty"`
	LLMPlayground string `json:"llmPlayground,omitempty"`
	MCPPlayground string `json:"mcpPlayground,omitempty"`
}

type ConnectSummary struct {
	Health    SourceHealth     `json:"health"`
	Counts    []ConnectCount   `json:"counts"`
	Analytics GatewayAnalytics `json:"analytics"`
	Links     ConsoleLinks     `json:"links"`
}

type ConnectSummaryEnvelope struct {
	Data ConnectSummary `json:"data"`
	Meta Meta           `json:"meta"`
}

type ResourcePage[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"nextCursor"`
	Total      int     `json:"total"`
}

type ResourcePageEnvelope[T any] struct {
	Data ResourcePage[T] `json:"data"`
	Meta Meta            `json:"meta"`
}

type ResourceEnvelope[T any] struct {
	Data T    `json:"data"`
	Meta Meta `json:"meta"`
}

type ConnectSetup struct {
	Source                Source       `json:"source"`
	ManagementConfigured  bool         `json:"managementConfigured"`
	ConfigurationReadable bool         `json:"configurationReadable"`
	Status                HealthStatus `json:"status"`
	Version               string       `json:"version,omitempty"`
	LatencyMS             *int64       `json:"latencyMs"`
	CheckedAt             time.Time    `json:"checkedAt"`
	Message               string       `json:"message,omitempty"`
	Links                 ConsoleLinks `json:"links"`
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
