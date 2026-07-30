package model

import "time"

type DemoScenario string

const (
	DemoScenarioHappy    DemoScenario = "happy"
	DemoScenarioApproval DemoScenario = "approval"
	DemoScenarioFailure  DemoScenario = "failure"
)

type DemoRunStatus string

const (
	DemoRunQueued          DemoRunStatus = "queued"
	DemoRunStarting        DemoRunStatus = "starting"
	DemoRunRunning         DemoRunStatus = "running"
	DemoRunWaitingApproval DemoRunStatus = "waiting_approval"
	DemoRunSucceeded       DemoRunStatus = "succeeded"
	DemoRunFailed          DemoRunStatus = "failed"
	DemoRunCancelled       DemoRunStatus = "cancelled"
	DemoRunInterrupted     DemoRunStatus = "interrupted"
	DemoRunExpired         DemoRunStatus = "expired"
)

type DemoRunOutcome string

const (
	DemoOutcomeNone      DemoRunOutcome = "none"
	DemoOutcomeNormal    DemoRunOutcome = "normal"
	DemoOutcomeApproved  DemoRunOutcome = "approved"
	DemoOutcomeDenied    DemoRunOutcome = "denied"
	DemoOutcomeDegraded  DemoRunOutcome = "degraded"
	DemoOutcomeCancelled DemoRunOutcome = "cancelled"
	DemoOutcomeFailed    DemoRunOutcome = "failed"
)

type DemoMetrics struct {
	LLMCalls       int `json:"llmCalls"`
	MCPCalls       int `json:"mcpCalls"`
	LocalToolCalls int `json:"localToolCalls"`
	A2ACalls       int `json:"a2aCalls"`
	HumanChecks    int `json:"humanChecks"`
	ErrorCount     int `json:"errorCount"`
}

type DemoApproval struct {
	TicketID         string    `json:"ticketId"`
	UpstreamID       string    `json:"upstreamId"`
	Source           Source    `json:"source"`
	FetchedAt        time.Time `json:"fetchedAt"`
	RawRef           RawRef    `json:"rawRef"`
	SessionID        string    `json:"sessionId"`
	AgentID          string    `json:"agentId,omitempty"`
	AgentUpstreamID  string    `json:"agentUpstreamId,omitempty"`
	EventID          string    `json:"eventId,omitempty"`
	EventType        string    `json:"eventType"`
	Tool             string    `json:"tool,omitempty"`
	Phase            string    `json:"phase"`
	Action           string    `json:"action"`
	Reason           string    `json:"reason,omitempty"`
	RiskScore        float64   `json:"riskScore"`
	MatchedRules     []string  `json:"matchedRules"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"createdAt"`
	CorrelationBasis string    `json:"correlationBasis"`
}

type DemoCorrelationEvidence struct {
	Status string `json:"status"`
	Basis  string `json:"basis"`
	Value  string `json:"value,omitempty"`
}

type DemoCorrelations struct {
	RunID       string                  `json:"runId"`
	TaskID      string                  `json:"taskId"`
	SessionID   string                  `json:"sessionId"`
	Trace       DemoCorrelationEvidence `json:"trace"`
	Approval    DemoCorrelationEvidence `json:"approval"`
	GatewayLogs DemoCorrelationEvidence `json:"gatewayLogs"`
}

type DemoLinks struct {
	Trace       string `json:"trace,omitempty"`
	Audit       string `json:"audit"`
	Approval    string `json:"approval,omitempty"`
	GatewayLogs string `json:"gatewayLogs,omitempty"`
}

// DemoRun contains only bounded Demo control state. Source-owned prompt,
// completion, tool argument, and authorization payloads are never copied here.
type DemoRun struct {
	RunID            string           `json:"runId"`
	Scenario         DemoScenario     `json:"scenario"`
	Status           DemoRunStatus    `json:"status"`
	Outcome          DemoRunOutcome   `json:"outcome"`
	StatusReasonCode string           `json:"statusReasonCode,omitempty"`
	RequestedAt      time.Time        `json:"requestedAt"`
	StartedAt        *time.Time       `json:"startedAt"`
	CompletedAt      *time.Time       `json:"completedAt"`
	LastHeartbeatAt  *time.Time       `json:"lastHeartbeatAt"`
	RunVersion       int64            `json:"runVersion"`
	DelayMS          int              `json:"delayMs"`
	TaskID           string           `json:"taskId"`
	SessionID        string           `json:"sessionId"`
	TraceID          string           `json:"traceId,omitempty"`
	RootSpanID       string           `json:"rootSpanId,omitempty"`
	RootAgentID      string           `json:"rootAgentId"`
	Approval         *DemoApproval    `json:"approval,omitempty"`
	CurrentStep      string           `json:"currentStep,omitempty"`
	CompletedSteps   int              `json:"completedSteps"`
	TotalSteps       int              `json:"totalSteps"`
	FixtureVersion   string           `json:"fixtureVersion"`
	RequestID        string           `json:"-"`
	ErrorCode        string           `json:"errorCode,omitempty"`
	ErrorSummary     string           `json:"errorSummary,omitempty"`
	CreatedBy        string           `json:"-"`
	ExpectedMetrics  DemoMetrics      `json:"expectedMetrics"`
	ObservedMetrics  *DemoMetrics     `json:"observedMetrics"`
	Correlations     DemoCorrelations `json:"correlations"`
	Links            DemoLinks        `json:"links"`
}

type DemoRunPage struct {
	Items      []DemoRun `json:"items"`
	NextCursor *string   `json:"nextCursor"`
	Total      int       `json:"total"`
}

type DemoRunEnvelope struct {
	Data DemoRun `json:"data"`
	Meta Meta    `json:"meta"`
}

type DemoRunListEnvelope struct {
	Data DemoRunPage `json:"data"`
	Meta Meta        `json:"meta"`
}

type DemoScenarioDefinition struct {
	ID              DemoScenario `json:"id"`
	Label           string       `json:"label"`
	Description     string       `json:"description"`
	ExpectedMetrics DemoMetrics  `json:"expectedMetrics"`
}

type DemoScenariosEnvelope struct {
	Data []DemoScenarioDefinition `json:"data"`
	Meta Meta                     `json:"meta"`
}

type DemoStatusComponent struct {
	ID          string       `json:"id"`
	Label       string       `json:"label"`
	Status      HealthStatus `json:"status"`
	Required    bool         `json:"required"`
	CheckedAt   time.Time    `json:"checkedAt"`
	Message     string       `json:"message,omitempty"`
	Remediation string       `json:"remediation,omitempty"`
}

type DemoStatus struct {
	Enabled        bool                  `json:"enabled"`
	Ready          bool                  `json:"ready"`
	ActiveRunID    *string               `json:"activeRunId"`
	MaxConcurrency int                   `json:"maxConcurrency"`
	Components     []DemoStatusComponent `json:"components"`
}

type DemoStatusEnvelope struct {
	Data DemoStatus `json:"data"`
	Meta Meta       `json:"meta"`
}

type DemoCreateRunRequest struct {
	Scenario DemoScenario `json:"scenario"`
	DelayMS  *int         `json:"delayMs"`
}

type DemoCancelRunRequest struct {
	Confirmed bool   `json:"confirm"`
	Note      string `json:"note"`
}

type DemoRunEvent struct {
	RunID          string         `json:"runId"`
	Type           string         `json:"type"`
	Status         DemoRunStatus  `json:"status,omitempty"`
	Outcome        DemoRunOutcome `json:"outcome,omitempty"`
	RunVersion     int64          `json:"runVersion"`
	StepID         string         `json:"stepId,omitempty"`
	TraceID        string         `json:"traceId,omitempty"`
	RootSpanID     string         `json:"rootSpanId,omitempty"`
	Approval       *DemoApproval  `json:"approval,omitempty"`
	CompletedSteps int            `json:"completedSteps,omitempty"`
	TotalSteps     int            `json:"totalSteps,omitempty"`
	OccurredAt     time.Time      `json:"occurredAt"`
}
