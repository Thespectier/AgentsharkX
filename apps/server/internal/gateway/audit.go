package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/model"
	"github.com/Thespectier/AgentsharkX/apps/server/internal/upstream"
)

type rawLogSearch struct {
	Logs       []rawLogEntry `json:"logs"`
	NextCursor *string       `json:"nextCursor"`
}

type rawLogEntry struct {
	ID          string        `json:"id"`
	StartedAt   time.Time     `json:"startedAt"`
	CompletedAt time.Time     `json:"completedAt"`
	DurationMS  int64         `json:"durationMs"`
	TraceID     *string       `json:"traceId"`
	SpanID      *string       `json:"spanId"`
	HTTPStatus  *int64        `json:"httpStatus"`
	Error       fieldPresence `json:"error"`
	GenAI       struct {
		OperationName *string `json:"operationName"`
		ProviderName  *string `json:"providerName"`
		RequestModel  *string `json:"requestModel"`
		ResponseModel *string `json:"responseModel"`
	} `json:"genAi"`
	Usage struct {
		InputTokens  *int64 `json:"inputTokens"`
		OutputTokens *int64 `json:"outputTokens"`
		TotalTokens  *int64 `json:"totalTokens"`
	} `json:"usage"`
	Cost       *float64      `json:"cost"`
	HasPayload bool          `json:"hasPayload"`
	Attributes fieldPresence `json:"attributes"`
	Payload    fieldPresence `json:"payload"`
}

type fieldPresence struct{ Present bool }

func (presence *fieldPresence) UnmarshalJSON(data []byte) error {
	presence.Present = string(data) != "null"
	return nil
}

// Traffic reads the verified request-log search API without requesting
// attributes or payloads. A missing log database is represented as an
// unavailable feed so the AgentGuard side can remain useful.
func (client *Client) Traffic(ctx context.Context, limit int) (model.AuditFeed, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	var response rawLogSearch
	_, err := client.upstream.PostJSON(ctx, "/api/logs/search", struct {
		Limit             int  `json:"limit"`
		IncludeAttributes bool `json:"includeAttributes"`
	}{Limit: limit, IncludeAttributes: false}, &response)
	if err != nil {
		var upstreamError *upstream.Error
		if errors.As(err, &upstreamError) && (upstreamError.Status == http.StatusNotFound || upstreamError.Status == http.StatusMethodNotAllowed || upstreamError.Status >= 500) {
			reason := "request-log storage is not configured or unavailable"
			if upstreamError.Status == http.StatusNotFound || upstreamError.Status == http.StatusMethodNotAllowed {
				reason = "request-log search capability is unavailable"
			}
			return model.AuditFeed{Status: "unavailable", Reason: reason, Events: []model.UnifiedEvent{}}, nil
		}
		return model.AuditFeed{}, err
	}
	if response.Logs == nil {
		return model.AuditFeed{}, &ContractError{Field: "/api/logs/search/logs", Problem: "required array is missing"}
	}
	events := make([]model.UnifiedEvent, 0, len(response.Logs))
	records := make([]model.AuditTrafficRecord, 0, len(response.Logs))
	for index, entry := range response.Logs {
		field := fmt.Sprintf("/api/logs/search/logs/%d", index)
		if entry.ID == "" {
			return model.AuditFeed{}, &ContractError{Field: field + "/id", Problem: "required field is missing"}
		}
		if entry.CompletedAt.IsZero() {
			return model.AuditFeed{}, &ContractError{Field: field + "/completedAt", Problem: "required timestamp is missing"}
		}
		if entry.StartedAt.IsZero() {
			return model.AuditFeed{}, &ContractError{Field: field + "/startedAt", Problem: "required timestamp is missing"}
		}
		if entry.Attributes.Present {
			return model.AuditFeed{}, &ContractError{Field: field + "/attributes", Problem: "redacted search unexpectedly returned attributes"}
		}
		if entry.Payload.Present {
			return model.AuditFeed{}, &ContractError{Field: field + "/payload", Problem: "redacted search unexpectedly returned payload"}
		}
		events = append(events, normalizeLog(entry))
		action := "OK"
		if entry.Error.Present || (entry.HTTPStatus != nil && *entry.HTTPStatus >= 500) {
			action = "ERROR"
		}
		records = append(records, model.AuditTrafficRecord{
			Timestamp: entry.CompletedAt, Action: action, LatencyMS: float64(entry.DurationMS),
		})
	}
	return model.AuditFeed{Status: "available", Events: events, Traffic: records}, nil
}

func normalizeLog(entry rawLogEntry) model.UnifiedEvent {
	severity := "info"
	if entry.Error.Present || (entry.HTTPStatus != nil && *entry.HTTPStatus >= 500) {
		severity = "high"
	} else if entry.HTTPStatus != nil && *entry.HTTPStatus >= 400 {
		severity = "medium"
	}
	provider := stringValue(entry.GenAI.ProviderName)
	modelName := stringValue(entry.GenAI.ResponseModel)
	if modelName == "" {
		modelName = stringValue(entry.GenAI.RequestModel)
	}
	subject := "Gateway request"
	if provider != "" {
		subject = provider + " request"
	}
	if modelName != "" {
		subject += " to " + modelName
	}
	if entry.HTTPStatus != nil {
		subject += fmt.Sprintf(" completed with HTTP %d", *entry.HTTPStatus)
	} else {
		subject += " completed"
	}
	correlation := (*model.EventCorrelation)(nil)
	if entry.TraceID != nil && strings.TrimSpace(*entry.TraceID) != "" {
		correlation = &model.EventCorrelation{TraceID: *entry.TraceID, Verified: false}
	}
	raw := map[string]any{
		"id": entry.ID, "startedAt": entry.StartedAt, "completedAt": entry.CompletedAt,
		"durationMs": entry.DurationMS, "hasPayload": entry.HasPayload,
		"genAi": map[string]any{
			"operationName": entry.GenAI.OperationName, "providerName": entry.GenAI.ProviderName,
			"requestModel": entry.GenAI.RequestModel, "responseModel": entry.GenAI.ResponseModel,
		},
		"usage": map[string]any{
			"inputTokens": entry.Usage.InputTokens, "outputTokens": entry.Usage.OutputTokens,
			"totalTokens": entry.Usage.TotalTokens,
		},
		"cost": entry.Cost, "httpStatus": entry.HTTPStatus, "errorPresent": entry.Error.Present,
	}
	if entry.TraceID != nil {
		raw["traceId"] = *entry.TraceID
	}
	if entry.SpanID != nil {
		raw["spanId"] = *entry.SpanID
	}
	return model.UnifiedEvent{
		ID: "gateway:" + entry.ID, Timestamp: entry.CompletedAt, Source: model.SourceAgentGateway,
		Kind: "traffic", Severity: severity, Target: &model.EventTarget{Provider: provider, Model: modelName},
		Correlation: correlation, Summary: subject,
		RawRef: model.RawRef{Source: model.SourceAgentGateway, ID: entry.ID}, Raw: raw,
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
