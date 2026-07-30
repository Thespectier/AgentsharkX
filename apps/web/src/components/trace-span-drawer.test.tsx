import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { mainTraceId, mockTraceDetails } from "../mocks/data";
import { TraceSpanDrawer } from "./trace-span-drawer";

const spanId = "2000000000000002";
const span = mockTraceDetails[mainTraceId].spans.find((item) => item.spanId === spanId)!;

function renderDrawer(traceId = mainTraceId) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TraceSpanDrawer onClose={vi.fn()} span={span} spanId={spanId} traceId={traceId} />
    </QueryClientProvider>,
  );
}

describe("TraceSpanDrawer", () => {
  it("retrieves and renders retained span detail on demand", async () => {
    renderDrawer();

    expect(screen.getByRole("dialog", { name: "Plan research" })).toBeVisible();
    expect(await screen.findByText(/Plan the verified inventory research task\./)).toBeVisible();
    expect(screen.getByText("Captured content is available below.")).toBeVisible();
    expect(screen.getByText(/first-token/)).toBeVisible();
  });

  it("does not offer a retry when retained content access is forbidden", async () => {
    renderDrawer("ffffffffffffffffffffffffffffffff");

    expect(
      await screen.findByRole("heading", { name: "Span content access denied" }),
    ).toBeVisible();
    expect(screen.getByText("This administrator cannot read retained content.")).toBeVisible();
    expect(screen.queryByRole("button", { name: /retry/i })).not.toBeInTheDocument();
  });
});
