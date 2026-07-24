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

type DiagnosticIssue struct {
	Source            Source       `json:"source"`
	Status            HealthStatus `json:"status"`
	Summary           string       `json:"summary"`
	Checks            []string     `json:"checks"`
	DocumentationPath string       `json:"documentationPath"`
}

type DiagnosticsData struct {
	Status HealthStatus      `json:"status"`
	Issues []DiagnosticIssue `json:"issues"`
}

type DiagnosticsEnvelope struct {
	Data DiagnosticsData `json:"data"`
	Meta Meta            `json:"meta"`
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

type Metric struct {
	ID      string  `json:"id"`
	Label   string  `json:"label"`
	Source  Source  `json:"source"`
	Value   float64 `json:"value"`
	Format  string  `json:"format"`
	Delta   float64 `json:"delta"`
	Trend   string  `json:"trend"`
	Tone    string  `json:"tone"`
	Context string  `json:"context"`
}

const (
	TrendBucketCount    = 12
	TrendBucketDuration = 5 * time.Minute
)

type TrendWindow struct {
	From           time.Time
	To             time.Time
	BucketDuration time.Duration
}

func CurrentTrendWindow(now time.Time) TrendWindow {
	end := now.UTC().Truncate(time.Second)
	return TrendWindow{
		From:           end.Add(-TrendBucketCount * TrendBucketDuration),
		To:             end,
		BucketDuration: TrendBucketDuration,
	}
}

type TrendPoint struct {
	Time           string   `json:"time"`
	Requests       float64  `json:"requests"`
	Latency        *float64 `json:"latency"`
	LatencySamples int      `json:"latencySamples"`
	Errors         float64  `json:"errors"`
	Denied         float64  `json:"denied"`
}

type OverviewData struct {
	Environment string         `json:"environment"`
	Mode        string         `json:"mode"`
	Health      []SourceHealth `json:"health"`
	Metrics     []Metric       `json:"metrics"`
	Trend       []TrendPoint   `json:"trend"`
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
	Policies  []ProtectPolicy
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
	Console           string `json:"console,omitempty"`
	RawConfig         string `json:"rawConfig,omitempty"`
	CEL               string `json:"cel,omitempty"`
	LLMPlayground     string `json:"llmPlayground,omitempty"`
	MCPPlayground     string `json:"mcpPlayground,omitempty"`
	AgentGuardConsole string `json:"agentguardConsole,omitempty"`
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

type ProtectResourceBase struct {
	ID         string    `json:"id"`
	UpstreamID string    `json:"upstreamId"`
	Source     Source    `json:"source"`
	FetchedAt  time.Time `json:"fetchedAt"`
	RawRef     RawRef    `json:"rawRef"`
}

type ProtectPolicy struct {
	ProtectResourceBase
	Name   string `json:"name"`
	Type   string `json:"type"`
	Scope  string `json:"scope"`
	Phase  string `json:"phase"`
	Action string `json:"action"`
	Status string `json:"status"`
}

type RuntimeRule struct {
	ProtectResourceBase
	Name            string `json:"name"`
	AgentID         string `json:"agentId,omitempty"`
	AgentUpstreamID string `json:"agentUpstreamId,omitempty"`
	Scope           string `json:"scope"`
	Phase           string `json:"phase"`
	Action          string `json:"action"`
	Status          string `json:"status"`
	Severity        string `json:"severity,omitempty"`
	Category        string `json:"category,omitempty"`
	ToolPattern     string `json:"toolPattern,omitempty"`
	Reason          string `json:"reason,omitempty"`
	UserManaged     bool   `json:"userManaged"`
}

type ProtectPluginPhase struct {
	ProtectResourceBase
	AgentID                string   `json:"agentId"`
	AgentUpstreamID        string   `json:"agentUpstreamId"`
	Phase                  string   `json:"phase"`
	ConfigSource           string   `json:"configSource"`
	EnabledLocalPlugins    []string `json:"enabledLocalPlugins"`
	EnabledRemotePlugins   []string `json:"enabledRemotePlugins"`
	AvailableLocalPlugins  []string `json:"availableLocalPlugins"`
	AvailableRemotePlugins []string `json:"availableRemotePlugins"`
}

type ProtectPluginSnapshot struct {
	Items     []ProtectPluginPhase
	FetchedAt time.Time
	Failures  []SourceFailure
}

type ProtectSnapshot struct {
	GatewayPolicies []ProtectPolicy      `json:"gatewayPolicies"`
	RuntimeRules    []RuntimeRule        `json:"runtimeRules"`
	Plugins         []ProtectPluginPhase `json:"plugins"`
	Links           ConsoleLinks         `json:"links"`
}

type ProtectSnapshotEnvelope struct {
	Data ProtectSnapshot `json:"data"`
	Meta Meta            `json:"meta"`
}

type RuleCheckMessage struct {
	Message string `json:"message"`
}

type RuntimeRuleCheckRequest struct {
	Source string `json:"source"`
}

type RuntimeRuleCheck struct {
	Source      Source             `json:"source"`
	OK          bool               `json:"ok"`
	Publishable bool               `json:"publishable"`
	RuleCount   int                `json:"ruleCount"`
	Errors      []RuleCheckMessage `json:"errors"`
	Warnings    []RuleCheckMessage `json:"warnings"`
	Hints       []RuleCheckMessage `json:"hints"`
	CheckToken  string             `json:"checkToken,omitempty"`
	ExpiresAt   *time.Time         `json:"expiresAt"`
	RequestID   string             `json:"requestId"`
}

type RuntimeRulePublishRequest struct {
	Source     string `json:"source"`
	CheckToken string `json:"checkToken"`
	Note       string `json:"note"`
	Confirmed  bool   `json:"confirmed"`
}

type ConfirmedActionRequest struct {
	Note      string `json:"note"`
	Confirmed bool   `json:"confirmed"`
}

type Approval struct {
	ProtectResourceBase
	AgentID         string    `json:"agentId,omitempty"`
	AgentUpstreamID string    `json:"agentUpstreamId,omitempty"`
	SessionID       string    `json:"sessionId,omitempty"`
	UserID          string    `json:"userId,omitempty"`
	EventID         string    `json:"eventId,omitempty"`
	EventType       string    `json:"eventType"`
	Tool            string    `json:"tool,omitempty"`
	Phase           string    `json:"phase"`
	Action          string    `json:"action"`
	Reason          string    `json:"reason,omitempty"`
	RiskScore       float64   `json:"riskScore"`
	MatchedRules    []string  `json:"matchedRules"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
}

type ProtectMutationReceipt struct {
	Operation   string    `json:"operation"`
	Status      string    `json:"status"`
	Source      Source    `json:"source"`
	Target      string    `json:"target"`
	RequestID   string    `json:"requestId"`
	CompletedAt time.Time `json:"completedAt"`
	Message     string    `json:"message"`
}

type ProtectMutationEnvelope struct {
	Data ProtectMutationReceipt `json:"data"`
	Meta Meta                   `json:"meta"`
}

type UnifiedEvent struct {
	ID          string            `json:"id"`
	Timestamp   time.Time         `json:"timestamp"`
	Source      Source            `json:"source"`
	Kind        string            `json:"kind"`
	Severity    string            `json:"severity"`
	Subject     *EventSubject     `json:"subject,omitempty"`
	Target      *EventTarget      `json:"target,omitempty"`
	Phase       string            `json:"phase,omitempty"`
	Action      string            `json:"action,omitempty"`
	Decision    string            `json:"decision,omitempty"`
	Correlation *EventCorrelation `json:"correlation,omitempty"`
	Summary     string            `json:"summary"`
	RawRef      RawRef            `json:"rawRef"`
	Raw         map[string]any    `json:"raw,omitempty"`
}

type EventSubject struct {
	AgentID     string `json:"agentId,omitempty"`
	PrincipalID string `json:"principalId,omitempty"`
	SessionID   string `json:"sessionId,omitempty"`
}

type EventTarget struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Tool     string `json:"tool,omitempty"`
	Resource string `json:"resource,omitempty"`
}

type EventCorrelation struct {
	TraceID   string `json:"traceId,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	Verified  bool   `json:"verified"`
}

type AuditTrafficRecord struct {
	Timestamp time.Time
	Action    string
	LatencyMS float64
	Risk      float64
}

type AuditFeed struct {
	Status  string
	Reason  string
	Events  []UnifiedEvent
	Traffic []AuditTrafficRecord
}

type AuditSession struct {
	ID              string     `json:"id"`
	UpstreamID      string     `json:"upstreamId"`
	AgentID         string     `json:"agentId"`
	AgentUpstreamID string     `json:"agentUpstreamId"`
	Principal       string     `json:"principal,omitempty"`
	Events          int        `json:"events"`
	Denies          int        `json:"denies"`
	LastSeen        *time.Time `json:"lastSeen"`
	Status          string     `json:"status"`
	Source          Source     `json:"source"`
	RawRef          RawRef     `json:"rawRef"`
}

type AuditData struct {
	Metrics  []Metric       `json:"metrics"`
	Trend    []TrendPoint   `json:"trend"`
	Events   []UnifiedEvent `json:"events"`
	Sessions []AuditSession `json:"sessions"`
}

type AuditEnvelope struct {
	Data AuditData `json:"data"`
	Meta Meta      `json:"meta"`
}

type EventsPage struct {
	Items      []UnifiedEvent `json:"items"`
	NextCursor *string        `json:"nextCursor"`
	Total      int            `json:"total"`
}

type EventsEnvelope struct {
	Data EventsPage `json:"data"`
	Meta Meta       `json:"meta"`
}

type OperationalSnapshot struct {
	Metrics []Metric
	Trend   []TrendPoint
	Events  []UnifiedEvent
	Meta    Meta
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
