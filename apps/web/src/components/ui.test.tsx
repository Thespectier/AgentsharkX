import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createRef } from "react";
import { describe, expect, it, vi } from "vitest";

import { Button, DataTable, EmptyState, ErrorState, SourceBadge, StatusOrb } from "./ui";

describe("console primitives", () => {
  it("keeps source and status text available to assistive technology", () => {
    render(
      <div>
        <SourceBadge source="agentguard" />
        <StatusOrb label="AgentGuard degraded" status="degraded" />
      </div>,
    );

    expect(screen.getByText("AgentGuard")).toBeVisible();
    expect(screen.getByText("AgentGuard degraded")).toHaveClass("sr-only");
  });

  it("forwards button refs for focus-managed overlays", () => {
    const ref = createRef<HTMLButtonElement>();
    render(<Button ref={ref}>Review</Button>);
    expect(ref.current).toBe(screen.getByRole("button", { name: "Review" }));
  });

  it("opens an interactive row with Enter and Space", async () => {
    const onOpen = vi.fn();
    const user = userEvent.setup();
    render(
      <DataTable
        columns={[{ key: "name", header: "Name", render: (item) => item.name }]}
        data={[{ id: "event-1", name: "Denied request" }]}
        label="Security events"
        onRowClick={onOpen}
      />,
    );
    const row = screen.getByText("Denied request").closest("tr");
    expect(row).not.toBeNull();
    row?.focus();
    await user.keyboard("{Enter}");
    fireEvent.keyDown(row as HTMLTableRowElement, { key: " " });
    expect(onOpen).toHaveBeenCalledTimes(2);
  });

  it("renders actionable empty and failure states", async () => {
    const retry = vi.fn();
    const user = userEvent.setup();
    const { rerender } = render(
      <EmptyState description="No explicit source records." title="No records" />,
    );
    expect(screen.getByRole("heading", { name: "No records" })).toBeVisible();

    rerender(<ErrorState description="Upstream unavailable" onRetry={retry} />);
    await user.click(screen.getByRole("button", { name: /retry/i }));
    expect(retry).toHaveBeenCalledOnce();
    expect(screen.getByRole("alert")).toHaveTextContent("Upstream unavailable");
  });
});
