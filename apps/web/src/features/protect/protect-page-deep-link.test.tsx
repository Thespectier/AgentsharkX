import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ApprovalPageEnvelope } from "../../generated/api-client";
import { protectApprovals } from "../../mocks/data";
import { ApprovalsView } from "./protect-page";

describe("Protect approval deep-link behavior", () => {
  it("waits for the approval page and then opens the exact pending ticket from ticketId", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const envelope: ApprovalPageEnvelope = {
      data: { items: protectApprovals, nextCursor: null, total: protectApprovals.length },
      meta: { fetchedAt: "2026-07-30T08:00:00Z", stale: false },
    };

    const view = render(
      <QueryClientProvider client={client}>
        <ApprovalsView
          envelope={undefined}
          error={null}
          loading={true}
          targetTicketId={protectApprovals[0].id}
        />
      </QueryClientProvider>,
    );

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    view.rerender(
      <QueryClientProvider client={client}>
        <ApprovalsView
          envelope={envelope}
          error={null}
          loading={false}
          targetTicketId={protectApprovals[0].id}
        />
      </QueryClientProvider>,
    );

    expect(
      await screen.findByRole("dialog", { name: `Review ${protectApprovals[0].tool}` }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: /Confidential document targets an external recipient/ }),
    ).toHaveAttribute("aria-current", "true");
  });
});
