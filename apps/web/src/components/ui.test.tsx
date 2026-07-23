import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createRef } from "react";
import { describe, expect, it, vi } from "vitest";

import {
  Button,
  DataTable,
  DetailDrawer,
  EmptyState,
  ErrorState,
  SourceBadge,
  StatusOrb,
} from "./ui";

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

  it("returns drawer focus after the closing navigation settles", async () => {
    const triggerRef = createRef<HTMLButtonElement>();
    const onClose = vi.fn();
    const renderDrawer = (open: boolean) => (
      <>
        <button ref={triggerRef}>Selected event</button>
        <DetailDrawer
          eyebrow="AgentGuard"
          onClose={onClose}
          open={open}
          returnFocusRef={triggerRef}
          title="Denied request"
        >
          Event detail
        </DetailDrawer>
      </>
    );
    const { rerender } = render(renderDrawer(true));

    expect(screen.getByRole("button", { name: "Close drawer" })).toHaveFocus();
    rerender(renderDrawer(false));

    await waitFor(() => expect(triggerRef.current).toHaveFocus());
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
