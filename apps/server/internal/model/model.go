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

// OverviewData remains health-only through Phase 4. Later phases populate its
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

type TrustResourceBase struct {
	ID         string    `json:"id"`
	UpstreamID string    `json:"upstreamId"`
	Source     Source    `json:"source"`
	FetchedAt  time.Time `json:"fetchedAt"`
	RawRef     RawRef    `json:"rawRef"`
}

type TrustLabels struct {
	Boundary    string   `json:"boundary"`
	Sensitivity string   `json:"sensitivity"`
	Integrity   string   `json:"integrity"`
	Tags        []string `json:"tags"`
}

type TrustDetection struct {
	ResourceUpstreamID string   `json:"resourceUpstreamId,omitempty"`
	Name               string   `json:"name,omitempty"`
	Label              string   `json:"label,omitempty"`
	RiskLevel          string   `json:"riskLevel"`
	Capabilities       []string `json:"capabilities"`
	RiskLabels         []string `json:"riskLabels"`
	PolicyTargets      []string `json:"policyTargets"`
	SuggestedPlugins   []string `json:"suggestedPlugins"`
}

type TrustResource struct {
	TrustResourceBase
	Name                 string          `json:"name"`
	Type                 string          `json:"type"`
	OwnerAgentID         string          `json:"ownerAgentId"`
	OwnerAgentUpstreamID string          `json:"ownerAgentUpstreamId"`
	SessionID            string          `json:"sessionId,omitempty"`
	Description          string          `json:"description,omitempty"`
	Framework            string          `json:"framework,omitempty"`
	Transport            string          `json:"transport,omitempty"`
	Remote               *bool           `json:"remote,omitempty"`
	ToolCount            *int            `json:"toolCount,omitempty"`
	Labels               *TrustLabels    `json:"labels,omitempty"`
	Detection            *TrustDetection `json:"detection,omitempty"`
}

type TrustSession struct {
	TrustResourceBase
	AgentID         string     `json:"agentId"`
	AgentUpstreamID string     `json:"agentUpstreamId"`
	UserID          string     `json:"userId,omitempty"`
	LastSeen        *time.Time `json:"lastSeen"`
	Status          string     `json:"status"`
}

type TrustAgent struct {
	TrustResourceBase
	Name       string     `json:"name"`
	Framework  *string    `json:"framework"`
	Principal  *string    `json:"principal"`
	TrustLevel *string    `json:"trustLevel"`
	Status     string     `json:"status"`
	Sessions   int        `json:"sessions"`
	Tools      int        `json:"tools"`
	Skills     int        `json:"skills"`
	MCPs       int        `json:"mcps"`
	LastActive *time.Time `json:"lastActive"`
}

type TrustAgentWorkspace struct {
	Agent     TrustAgent      `json:"agent"`
	Sessions  []TrustSession  `json:"sessions"`
	Resources []TrustResource `json:"resources"`
}

type TrustSnapshot struct {
	Sessions  []TrustSession
	Resources []TrustResource
	FetchedAt time.Time
	Failures  []SourceFailure
}

type TrustLabelUpdate struct {
	Boundary    *string   `json:"boundary,omitempty"`
	Sensitivity *string   `json:"sensitivity,omitempty"`
	Integrity   *string   `json:"integrity,omitempty"`
	Tags        *[]string `json:"tags,omitempty"`
}

type TrustDetectionRequest struct {
	ResourceIDs []string `json:"resourceIds"`
	UseLLM      bool     `json:"useLlm"`
}

type TrustScanError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type TrustScanJob struct {
	ID              string           `json:"id"`
	Source          Source           `json:"source"`
	AgentID         string           `json:"agentId"`
	AgentUpstreamID string           `json:"agentUpstreamId"`
	ResourceType    string           `json:"resourceType"`
	ResourceIDs     []string         `json:"resourceIds"`
	Status          string           `json:"status"`
	CreatedAt       time.Time        `json:"createdAt"`
	StartedAt       *time.Time       `json:"startedAt"`
	CompletedAt     *time.Time       `json:"completedAt"`
	UpdatedAt       time.Time        `json:"updatedAt"`
	Results         []TrustDetection `json:"results"`
	Warnings        []string         `json:"warnings"`
	Error           *TrustScanError  `json:"error,omitempty"`
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
