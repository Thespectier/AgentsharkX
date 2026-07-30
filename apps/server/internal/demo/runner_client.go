// Package demo implements the fixed Demo Lab control plane.
package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
)

var (
	ErrRunnerUnavailable = errors.New("demo runner is unavailable")
	ErrRunnerContract    = errors.New("demo runner contract mismatch")
	ErrRunnerNotFound    = errors.New("demo runner run was not found")
	ErrRunnerBusy        = errors.New("demo runner is busy")
	ErrRunnerConflict    = errors.New("demo runner run conflicts with persisted state")
)

type RunnerStartRequest struct {
	RunID     string             `json:"runId"`
	Scenario  model.DemoScenario `json:"scenario"`
	DelayMS   int                `json:"delayMs"`
	TaskID    string             `json:"taskId"`
	SessionID string             `json:"sessionId"`
	RequestID string             `json:"requestId"`
}

type RunnerMetrics struct {
	LLMCalls       int `json:"llmCalls"`
	MCPCalls       int `json:"mcpCalls"`
	LocalToolCalls int `json:"localToolCalls"`
	A2ACalls       int `json:"a2aCalls"`
	ErrorCount     int `json:"errorCount"`
}

type RunnerSnapshot struct {
	RunID           string               `json:"runId"`
	Scenario        model.DemoScenario   `json:"scenario"`
	Status          string               `json:"status"`
	Outcome         model.DemoRunOutcome `json:"outcome"`
	DelayMS         int                  `json:"delayMs"`
	TaskID          string               `json:"taskId"`
	SessionID       string               `json:"sessionId"`
	RequestID       string               `json:"requestId"`
	TraceID         string               `json:"traceId"`
	RootSpanID      string               `json:"rootSpanId"`
	CurrentStep     string               `json:"currentStep"`
	CompletedSteps  int                  `json:"completedSteps"`
	TotalSteps      int                  `json:"totalSteps"`
	StartedAt       *time.Time           `json:"startedAt"`
	CompletedAt     *time.Time           `json:"completedAt"`
	HeartbeatAt     time.Time            `json:"heartbeatAt"`
	CancelRequested bool                 `json:"cancelRequested"`
	ErrorCode       string               `json:"errorCode"`
	ErrorSummary    string               `json:"errorSummary"`
	Metrics         *RunnerMetrics       `json:"metrics"`
}

type RunnerHealth struct {
	Status         string  `json:"status"`
	Service        string  `json:"service"`
	MaxConcurrency int     `json:"maxConcurrency"`
	ActiveRunID    *string `json:"activeRunId"`
}

type Runner interface {
	Health(context.Context) (RunnerHealth, error)
	Start(context.Context, RunnerStartRequest) (RunnerSnapshot, error)
	Get(context.Context, string) (RunnerSnapshot, error)
	Cancel(context.Context, string) (RunnerSnapshot, error)
}

type RunnerClient struct {
	baseURL *url.URL
	token   string
	http    *http.Client
}

func NewRunnerClient(rawURL, token string, client *http.Client) (*RunnerClient, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(rawURL), "/") + "/")
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("demo runner URL must be an absolute credential-free HTTP(S) URL")
	}
	if len([]byte(token)) < 32 {
		return nil, errors.New("demo runner token must contain at least 32 bytes")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &RunnerClient{baseURL: parsed, token: token, http: client}, nil
}

func (client *RunnerClient) Health(ctx context.Context) (RunnerHealth, error) {
	var health RunnerHealth
	if err := client.do(ctx, http.MethodGet, "healthz", nil, &health, false); err != nil {
		return RunnerHealth{}, err
	}
	if health.Status != "healthy" || health.Service != "agentshark-demo-runner" || health.MaxConcurrency != 1 {
		return RunnerHealth{}, ErrRunnerContract
	}
	return health, nil
}

func (client *RunnerClient) Start(ctx context.Context, request RunnerStartRequest) (RunnerSnapshot, error) {
	var snapshot RunnerSnapshot
	if err := client.do(ctx, http.MethodPost, "internal/v1/runs", request, &snapshot, true); err != nil {
		return RunnerSnapshot{}, err
	}
	return snapshot, nil
}

func (client *RunnerClient) Get(ctx context.Context, runID string) (RunnerSnapshot, error) {
	var snapshot RunnerSnapshot
	path := "internal/v1/runs/" + url.PathEscape(strings.TrimSpace(runID))
	if err := client.do(ctx, http.MethodGet, path, nil, &snapshot, true); err != nil {
		return RunnerSnapshot{}, err
	}
	return snapshot, nil
}

func (client *RunnerClient) Cancel(ctx context.Context, runID string) (RunnerSnapshot, error) {
	var snapshot RunnerSnapshot
	path := "internal/v1/runs/" + url.PathEscape(strings.TrimSpace(runID)) + "/cancel"
	if err := client.do(ctx, http.MethodPost, path, struct{}{}, &snapshot, true); err != nil {
		return RunnerSnapshot{}, err
	}
	return snapshot, nil
}

func (client *RunnerClient) do(ctx context.Context, method, path string, body, destination any, authenticated bool) error {
	var requestBody io.Reader
	if body != nil {
		document, err := json.Marshal(body)
		if err != nil {
			return err
		}
		requestBody = bytes.NewReader(document)
	}
	endpoint := client.baseURL.ResolveReference(&url.URL{Path: path})
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return ErrRunnerUnavailable
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if authenticated {
		request.Header.Set("Authorization", "Bearer "+client.token)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("%w: request failed", ErrRunnerUnavailable)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 64*1024)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var payload struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(limited).Decode(&payload)
		switch payload.Code {
		case "demo_run_not_found":
			return ErrRunnerNotFound
		case "demo_run_busy":
			return ErrRunnerBusy
		case "demo_run_id_conflict":
			return ErrRunnerConflict
		default:
			return fmt.Errorf("%w: status %d", ErrRunnerUnavailable, response.StatusCode)
		}
	}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("%w: invalid JSON", ErrRunnerContract)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: multiple JSON values", ErrRunnerContract)
	}
	return nil
}

var _ Runner = (*RunnerClient)(nil)
