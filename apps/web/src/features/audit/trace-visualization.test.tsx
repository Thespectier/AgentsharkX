import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { TraceDetail, TraceSpan } from "../../generated/api-client";
import { mainTraceId, mockTraceDetails } from "../../mocks/data";
import { TraceVisualization } from "./trace-visualization";

const trace = mockTraceDetails[mainTraceId];

describe("TraceVisualization", () => {
  it("keeps Flow as the default and switches to Timeline on one global time scale", async () => {
    const user = userEvent.setup();
    render(<TraceVisualization isLive={false} onSelectSpan={vi.fn()} trace={trace} />);

    expect(screen.getByRole("button", { name: "Flow" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("group", { name: "Trace flow lanes" })).toBeVisible();

    await user.click(screen.getByRole("button", { name: "Timeline" }));

    expect(screen.getByRole("region", { name: "Trace timeline" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Timeline" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    const inventoryBar = screen.getByRole("button", {
      name: "Open timeline bar for Search inventory",
    }).firstElementChild;
    const peerBar = screen.getByRole("button", {
      name: "Open timeline bar for Delegate verification",
    }).firstElementChild;
    expect(inventoryBar).toHaveStyle({ left: `${(1_100 / 6_000) * 100}%` });
    expect(peerBar).toHaveStyle({ left: `${(1_700 / 6_000) * 100}%` });
  });

  it("filters Span types and changes Timeline zoom", async () => {
    const user = userEvent.setup();
    const view = render(
      <TraceVisualization
        defaultView="timeline"
        isLive={false}
        onSelectSpan={vi.fn()}
        trace={trace}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Toggle LLM spans" }));
    expect(
      screen.queryByRole("button", { name: "Open span Plan research" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "All" }));
    expect(screen.getByRole("button", { name: "Open span Plan research" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "2× zoom" }));
    expect(view.container.querySelector(".trace-timeline__scroll")).toHaveClass(
      "trace-timeline__scroll--2x",
    );
  });

  it("shares selection, highlights the ancestor path, and dims unrelated rows", async () => {
    const user = userEvent.setup();
    const onSelectSpan = vi.fn();
    const { container } = render(
      <TraceVisualization
        defaultView="timeline"
        isLive={false}
        onSelectSpan={onSelectSpan}
        selectedSpanId="5000000000000005"
        trace={trace}
      />,
    );

    expect(container.querySelector('[data-span-id="1000000000000001"]')).toHaveClass(
      "trace-timeline__row--path",
    );
    expect(container.querySelector('[data-span-id="4000000000000004"]')).toHaveClass(
      "trace-timeline__row--path",
    );
    expect(container.querySelector('[data-span-id="5000000000000005"]')).toHaveClass(
      "trace-timeline__row--selected",
    );
    expect(container.querySelector('[data-span-id="3000000000000003"]')).toHaveClass(
      "trace-timeline__row--dimmed",
    );

    await user.click(screen.getByRole("button", { name: "Open span Compose answer" }));
    expect(onSelectSpan).toHaveBeenCalledWith("8000000000000008", expect.any(HTMLElement));
  });

  it("does not add arrival motion when reduced motion is requested", () => {
    const view = render(
      <TraceVisualization
        defaultView="timeline"
        isLive={true}
        onSelectSpan={vi.fn()}
        trace={trace}
      />,
    );
    const added = {
      ...trace.spans[0],
      spanId: "9000000000000009",
      parentSpanId: trace.rootSpan?.spanId,
      name: "New live Span",
      startedAt: "2026-07-30T07:58:05Z",
      endedAt: null,
      durationMs: null,
      statusCode: "unset",
    } satisfies TraceSpan;
    const updated = {
      ...trace,
      summary: { ...trace.summary, status: "running", endedAt: undefined },
      spans: [...trace.spans, added],
      totalSpans: trace.totalSpans + 1,
    } satisfies TraceDetail;

    view.rerender(
      <TraceVisualization defaultView="timeline" isLive onSelectSpan={vi.fn()} trace={updated} />,
    );

    expect(view.container.querySelector(`[data-span-id="${added.spanId}"]`)).not.toHaveClass(
      "trace-timeline__row--new",
    );
  });
});
