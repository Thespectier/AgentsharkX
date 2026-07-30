import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";

import type { ProtectMutationReceipt } from "../../generated/api-client";
import { protectApprovals } from "../../mocks/data";
import { server } from "../../mocks/server";
import { ApprovalDecisionDialog, ProtectMutationReceiptNotice } from "./approval-decision";

const receipt: ProtectMutationReceipt = {
  operation: "approve-approval",
  status: "succeeded",
  source: "agentguard",
  target: protectApprovals[0].id,
  requestId: "req_approval_test",
  completedAt: "2026-07-30T08:00:00Z",
  message: "Approval ticket approved",
};

function response(data = receipt) {
  return HttpResponse.json({
    data,
    meta: {
      fetchedAt: data.completedAt,
      partial: false,
      sources: ["agentguard"],
      sourceFailures: [],
    },
  });
}

function renderDecision(onReceipt = vi.fn(), onClose = vi.fn()) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const result = render(
    <QueryClientProvider client={client}>
      <ApprovalDecisionDialog
        approval={protectApprovals[0]}
        onClose={onClose}
        onReceipt={onReceipt}
      />
    </QueryClientProvider>,
  );
  return { ...result, onClose, onReceipt };
}

async function completeConfirmation() {
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Operator note"), "Reviewed against the incident record.");
  await user.click(
    screen.getByLabelText("I confirm this operator decision for the selected pending ticket."),
  );
  return user;
}

describe("ApprovalDecisionDialog", () => {
  it("sends one explicit approval and returns its receipt", async () => {
    let requestBody: unknown;
    let requestCount = 0;
    server.use(
      http.post("/api/v1/protect/approvals/:ticketId/approve", async ({ request }) => {
        requestCount += 1;
        requestBody = await request.json();
        return response();
      }),
    );
    const { onClose, onReceipt } = renderDecision();
    const user = await completeConfirmation();

    await user.click(screen.getByRole("button", { name: "Approve" }));

    await waitFor(() => expect(onReceipt).toHaveBeenCalledWith(receipt));
    expect(onClose).toHaveBeenCalledOnce();
    expect(requestCount).toBe(1);
    expect(requestBody).toEqual({
      note: "Reviewed against the incident record.",
      confirmed: true,
    });
  });

  it("waits for a deliberate retry after an upstream timeout", async () => {
    let requestCount = 0;
    server.use(
      http.post("/api/v1/protect/approvals/:ticketId/approve", () => {
        requestCount += 1;
        if (requestCount === 1) {
          return HttpResponse.json(
            {
              error: {
                code: "UPSTREAM_UNAVAILABLE",
                message: "AgentGuard timed out. Confirm the ticket state before retrying.",
                source: "agentguard",
                requestId: "req_approval_timeout",
                retryable: true,
              },
            },
            { status: 503 },
          );
        }
        return response();
      }),
    );
    const { onClose, onReceipt } = renderDecision();
    const user = await completeConfirmation();

    await user.click(screen.getByRole("button", { name: "Approve" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("AgentGuard timed out");
    expect(requestCount).toBe(1);
    expect(onReceipt).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Retry approve" }));
    await waitFor(() => expect(onReceipt).toHaveBeenCalledWith(receipt));
    expect(requestCount).toBe(2);
    expect(onClose).toHaveBeenCalledOnce();
  });
});

describe("ProtectMutationReceiptNotice", () => {
  it("keeps the BFF request ID visible for audit lookup", () => {
    render(<ProtectMutationReceiptNotice receipt={receipt} />);

    expect(screen.getByRole("status")).toHaveTextContent("Approval ticket approved");
    expect(screen.getByRole("status")).toHaveTextContent("req_approval_test");
  });
});
