import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  buildTraceFlowLayout,
  TRACE_FLOW_NODE_LIMIT,
  TraceFlow,
  type TraceFlowSpan,
} from "./trace-flow";

const traceId = "11111111111111111111111111111111";

function span(spanId: string, options: Partial<TraceFlowSpan> = {}): TraceFlowSpan {
  return {
    traceId,
    spanId,
    name: `span-${spanId}`,
    startedAt: `2026-07-30T08:00:${spanId.slice(-2)}Z`,
    statusCode: "ok",
    ...options,
  };
}

describe("trace flow layout", () => {
  it("draws only explicit parent and Span Link edges", () => {
    const spans = [
      span("0000000000000001"),
      span("0000000000000002", { parentSpanId: "0000000000000001" }),
      span("0000000000000003"),
    ];
    const layout = buildTraceFlowLayout(spans, [
      {
        spanId: "0000000000000003",
        linkedTraceId: traceId,
        linkedSpanId: "0000000000000001",
      },
    ]);

    expect(layout.edges.map((edge) => [edge.kind, edge.sourceId, edge.targetId])).toEqual([
      ["parent", "0000000000000001", "0000000000000002"],
      ["link", "0000000000000003", "0000000000000001"],
    ]);
  });

  it("keeps the graph bounded and folds omitted spans by lane", () => {
    const spans = Array.from({ length: 240 }, (_, index) =>
      span(index.toString(16).padStart(16, "0").replace(/^0+$/, "1"), {
        openInferenceKind: index % 2 ? "LLM" : "TOOL",
      }),
    );
    const selected = spans.at(-1)!.spanId;
    const layout = buildTraceFlowLayout(spans, [], selected);

    expect(layout.nodes.length).toBeLessThanOrEqual(TRACE_FLOW_NODE_LIMIT);
    expect(layout.hiddenCount).toBeGreaterThan(0);
    expect(layout.nodes.some((node) => node.id === selected)).toBe(true);
    expect(layout.nodes.some((node) => node.foldedCount)).toBe(true);
  });

  it("uses distinct solid and dashed edge classes", () => {
    const spans = [
      span("0000000000000001"),
      span("0000000000000002", { parentSpanId: "0000000000000001" }),
      span("0000000000000003"),
    ];
    const { container } = render(
      <TraceFlow
        links={[
          {
            spanId: "0000000000000003",
            linkedTraceId: traceId,
            linkedSpanId: "0000000000000001",
          },
        ]}
        onSelect={() => undefined}
        spans={spans}
      />,
    );

    expect(container.querySelectorAll('[data-edge-kind="parent"]')).toHaveLength(1);
    expect(container.querySelectorAll('[data-edge-kind="link"]')).toHaveLength(1);
    expect(screen.getByText("Explicit parent")).toBeVisible();
    expect(screen.getByText("Span Link")).toBeVisible();
  });

  it("does not resolve an external Link through a colliding local Span ID", () => {
    const layout = buildTraceFlowLayout(
      [span("0000000000000001"), span("0000000000000002")],
      [
        {
          spanId: "0000000000000002",
          linkedTraceId: "99999999999999999999999999999999",
          linkedSpanId: "0000000000000001",
        },
      ],
    );

    expect(layout.edges).toEqual([
      expect.objectContaining({
        kind: "link",
        sourceId: "0000000000000002",
        targetId: undefined,
        externalLabel: expect.any(String),
      }),
    ]);
  });

  it("does not apply arrival animation when reduced motion is requested", () => {
    const first = span("0000000000000001");
    const added = span("0000000000000002");
    const view = render(<TraceFlow links={[]} onSelect={() => undefined} spans={[first]} />);
    view.rerender(<TraceFlow links={[]} onSelect={() => undefined} spans={[first, added]} />);

    expect(screen.getByRole("button", { name: `Open span ${added.name}` })).not.toHaveClass(
      "trace-flow__node--new",
    );
  });
});
