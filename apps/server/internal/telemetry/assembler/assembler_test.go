package assembler

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/Thespectier/AgentsharkX/apps/server/internal/telemetry"
)

const assemblerTraceID = "11111111111111111111111111111111"

var assemblerNow = time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

func TestAssembleTaskRootStates(t *testing.T) {
	tests := []struct {
		name             string
		mutate           func(*telemetry.Span)
		withoutRoot      bool
		wantStatus       string
		wantCompleteness string
		wantDuration     bool
	}{
		{name: "succeeded", wantStatus: StatusSucceeded, wantCompleteness: CompletenessVerified, wantDuration: true},
		{name: "failed", mutate: func(span *telemetry.Span) { span.StatusCode = telemetry.StatusError }, wantStatus: StatusFailed, wantCompleteness: CompletenessVerified, wantDuration: true},
		{name: "running", mutate: func(span *telemetry.Span) { span.EndedAt = nil; span.DurationMS = nil }, wantStatus: StatusRunning, wantCompleteness: CompletenessPartial},
		{name: "missing root", withoutRoot: true, wantStatus: StatusUnknown, wantCompleteness: CompletenessPartial},
		{name: "missing task identifier", mutate: func(span *telemetry.Span) { span.TaskID = "" }, wantStatus: StatusSucceeded, wantCompleteness: CompletenessPartial, wantDuration: true},
		{name: "root has parent", mutate: func(span *telemetry.Span) { span.ParentSpanID = "abababababababab" }, wantStatus: StatusSucceeded, wantCompleteness: CompletenessPartial, wantDuration: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := summarySpan(1, "AGENT", false)
			root.Name = "task"
			root.AgentID, root.SessionID, root.TaskID = "agent-1", "session-1", "task-1"
			root.Attributes[telemetry.AttributeTaskRoot] = true
			if test.withoutRoot {
				delete(root.Attributes, telemetry.AttributeTaskRoot)
			}
			if test.mutate != nil {
				test.mutate(&root)
			}
			summary, err := Assemble(assemblerTraceID, []telemetry.Span{root}, assemblerNow)
			if err != nil {
				t.Fatal(err)
			}
			if summary.Status != test.wantStatus || summary.Completeness != test.wantCompleteness ||
				(summary.DurationMS != nil) != test.wantDuration {
				t.Fatalf("summary = %#v", summary)
			}
			if !test.withoutRoot && (summary.RootSpanID != root.SpanID || summary.StartedAt != root.StartedAt) {
				t.Fatalf("root projection = %#v", summary)
			}
			if test.withoutRoot && (summary.RootSpanID != "" || summary.DurationMS != nil || summary.EndedAt != nil) {
				t.Fatalf("observed duration was presented as task duration: %#v", summary)
			}
		})
	}
}

func TestAssembleCountsLLMMCPToolRetrieverAndA2AExactly(t *testing.T) {
	root := summarySpan(1, "AGENT", false)
	root.Attributes[telemetry.AttributeTaskRoot] = true
	root.AgentID, root.SessionID, root.TaskID = "root", "session", "task"
	spans := []telemetry.Span{root}
	for index := 0; index < 3; index++ {
		span := summarySpan(index+2, "LLM", true)
		input, output := int64(index+1), int64(index+2)
		span.InputTokens, span.OutputTokens = &input, &output
		if index == 0 {
			span.StatusCode = telemetry.StatusError
		}
		spans = append(spans, span)
	}
	for index := 0; index < 2; index++ {
		span := summarySpan(index+5, "TOOL", true)
		span.ToolKind = "mcp"
		span.Attributes[telemetry.AttributeMCPMethod] = "tools/call"
		spans = append(spans, span)
	}
	local := summarySpan(7, "TOOL", true)
	spans = append(spans, local)
	retriever := summarySpan(8, "RETRIEVER", true)
	spans = append(spans, retriever)
	a2a := summarySpan(9, "AGENT", true)
	a2a.PeerAgentID = "planner"
	a2a.Attributes["gen_ai.operation.name"] = "invoke_agent"
	spans = append(spans, a2a)
	for index, method := range []string{"initialize", "tools/list"} {
		protocol := summarySpan(index+10, "TOOL", true)
		protocol.ToolKind = "mcp"
		protocol.Attributes[telemetry.AttributeMCPMethod] = method
		spans = append(spans, protocol)
	}
	toolHook := summarySpan(12, "TOOL", false)
	toolHook.Name = "tool.before"
	spans = append(spans, toolHook)
	lowLevelHTTP := summarySpan(13, "LLM", false)
	lowLevelHTTP.Name = "POST https://provider.example"
	spans = append(spans, lowLevelHTTP)

	summary, err := Assemble(assemblerTraceID, spans, assemblerNow)
	if err != nil {
		t.Fatal(err)
	}
	if summary.LLMCalls != 3 || summary.MCPCalls != 2 || summary.LocalToolCalls != 1 ||
		summary.ToolCalls != 3 || summary.A2ACalls != 1 || summary.RetrieverCalls != 1 || summary.ErrorCount != 1 {
		t.Fatalf("interaction counts = %#v", summary)
	}
	if summary.InputTokens != 6 || summary.OutputTokens != 9 || summary.TotalTokens != 15 {
		t.Fatalf("token counts = %#v", summary)
	}
	if summary.SpanCount != len(spans) {
		t.Fatalf("span count = %d, want %d", summary.SpanCount, len(spans))
	}
}

func TestAssembleMultipleTaskRootsArePartialAndDeterministic(t *testing.T) {
	first := summarySpan(2, "AGENT", false)
	first.Attributes[telemetry.AttributeTaskRoot] = true
	first.AgentID, first.SessionID, first.TaskID = "first", "session", "task"
	second := summarySpan(1, "AGENT", false)
	second.Attributes[telemetry.AttributeTaskRoot] = true
	second.AgentID, second.SessionID, second.TaskID = "second", "session", "task"
	summary, err := Assemble(assemblerTraceID, []telemetry.Span{first, second}, assemblerNow)
	if err != nil {
		t.Fatal(err)
	}
	if summary.RootSpanID != second.SpanID || summary.RootAgentID != "second" ||
		summary.Status != StatusSucceeded || summary.Completeness != CompletenessPartial ||
		summary.DurationMS == nil || *summary.DurationMS != 1000 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestAssembleMultipleTaskRootsPrioritizesAnUnfinishedRoot(t *testing.T) {
	finished := summarySpan(1, "AGENT", false)
	finished.Attributes[telemetry.AttributeTaskRoot] = true
	finished.AgentID, finished.SessionID, finished.TaskID = "finished", "session", "finished-task"
	running := summarySpan(2, "AGENT", false)
	running.Attributes[telemetry.AttributeTaskRoot] = true
	running.AgentID, running.SessionID, running.TaskID = "running", "session", "running-task"
	running.EndedAt = nil
	running.DurationMS = nil

	summary, err := Assemble(assemblerTraceID, []telemetry.Span{finished, running}, assemblerNow)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != StatusRunning || summary.Completeness != CompletenessPartial ||
		summary.RootSpanID != running.SpanID || summary.EndedAt != nil || summary.DurationMS != nil {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestAssembleSaturatesTokenCountersAtInt64Maximum(t *testing.T) {
	first := summarySpan(1, "LLM", true)
	firstInput, firstOutput := int64(math.MaxInt64), int64(math.MaxInt64-1)
	first.InputTokens, first.OutputTokens = &firstInput, &firstOutput
	second := summarySpan(2, "LLM", true)
	secondInput, secondOutput := int64(1), int64(2)
	second.InputTokens, second.OutputTokens = &secondInput, &secondOutput

	summary, err := Assemble(assemblerTraceID, []telemetry.Span{first, second}, assemblerNow)
	if err != nil {
		t.Fatal(err)
	}
	if summary.InputTokens != math.MaxInt64 || summary.OutputTokens != math.MaxInt64 ||
		summary.TotalTokens != math.MaxInt64 {
		t.Fatalf("saturated token counts = %#v", summary)
	}
}

func summarySpan(index int, kind string, countable bool) telemetry.Span {
	startedAt := assemblerNow.Add(time.Duration(index) * time.Second)
	endedAt := startedAt.Add(time.Second)
	duration := int64(1000)
	return telemetry.Span{
		TraceID: assemblerTraceID, SpanID: fmt.Sprintf("%016x", index), Name: fmt.Sprintf("span-%d", index),
		OpenInferenceKind: kind, StartedAt: startedAt, EndedAt: &endedAt, DurationMS: &duration,
		StatusCode: telemetry.StatusOK, Countable: countable, ContentState: telemetry.ContentStateNotCollected,
		Attributes: map[string]any{}, Resource: map[string]any{}, Events: []telemetry.Event{},
	}
}
