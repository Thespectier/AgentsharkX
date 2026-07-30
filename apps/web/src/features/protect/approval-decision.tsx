import { useMutation, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, LoaderCircle, ShieldAlert } from "lucide-react";
import { useState } from "react";

import { Button, Dialog, SeverityBadge, SourceBadge } from "../../components/ui";
import type { Approval, ProtectMutationReceipt } from "../../generated/api-client";
import { ApiError, formatError, mutateOperation } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { synchronizeAgentGuardData } from "../../lib/query-sync";
import type { Severity } from "../../types";

export function ApprovalDecisionDialog({
  approval,
  onClose,
  onReceipt,
}: {
  approval: Approval;
  onClose: () => void;
  onReceipt: (receipt: ProtectMutationReceipt) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [note, setNote] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [decision, setDecision] = useState<"approve" | "deny">("approve");
  const mutation = useMutation({
    mutationFn: (action: "approve" | "deny") =>
      mutateOperation(
        action === "approve" ? "approveTicket" : "denyTicket",
        { note, confirmed },
        { path: { ticketId: approval.id } },
      ),
    onSuccess: (response) => {
      onReceipt(response.data);
      onClose();
      void synchronizeAgentGuardData(queryClient);
    },
    onError: (mutationError) => {
      if (mutationError instanceof ApiError && mutationError.status === 404) {
        void synchronizeAgentGuardData(queryClient);
      }
    },
  });
  const decide = (action: "approve" | "deny") => {
    setDecision(action);
    mutation.mutate(action);
  };

  return (
    <Dialog
      description="Review sanitized AgentGuard context, write a note, and explicitly confirm one decision."
      onClose={() => !mutation.isPending && onClose()}
      open
      title={`Review ${approval.tool || approval.eventType}`}
    >
      <div className="dialog-form">
        <div className="approval-dialog-summary">
          <SeverityBadge severity={approvalSeverity(approval.riskScore)} />
          <code>{approval.id}</code>
          <p>{approval.reason || "No upstream reason was provided."}</p>
          <SourceBadge source={approval.source} />
          <p>
            Phase: {approval.phase} · Matched rules:{" "}
            {approval.matchedRules.join(", ") || "none reported"}
          </p>
        </div>
        <label className="field">
          <span>{t("Operator note")}</span>
          <textarea
            aria-label={t("Operator note")}
            onChange={(event) => setNote(event.target.value)}
            rows={3}
            value={note}
          />
        </label>
        <label className="confirm-field">
          <input
            checked={confirmed}
            onChange={(event) => setConfirmed(event.target.checked)}
            type="checkbox"
          />
          I confirm this operator decision for the selected pending ticket.
        </label>
        {mutation.isError ? <ProtectMutationError error={mutation.error} /> : null}
        <footer>
          <Button disabled={mutation.isPending} onClick={onClose} variant="ghost">
            Cancel
          </Button>
          <Button
            disabled={!note.trim() || !confirmed || mutation.isPending}
            onClick={() => decide("deny")}
            variant="danger"
          >
            {mutation.isPending && decision === "deny" ? (
              <LoaderCircle className="spin" size={14} />
            ) : null}
            {mutation.isError && decision === "deny" ? "Retry deny" : "Deny"}
          </Button>
          <Button
            disabled={!note.trim() || !confirmed || mutation.isPending}
            onClick={() => decide("approve")}
            variant="primary"
          >
            {mutation.isPending && decision === "approve" ? (
              <LoaderCircle className="spin" size={14} />
            ) : null}
            {mutation.isError && decision === "approve" ? "Retry approve" : "Approve"}
          </Button>
        </footer>
      </div>
    </Dialog>
  );
}

export function ProtectMutationReceiptNotice({ receipt }: { receipt: ProtectMutationReceipt }) {
  return (
    <div className="mutation-receipt" role="status">
      <CheckCircle2 aria-hidden="true" size={16} />
      <div>
        <strong>{receipt.message}</strong>
        <span>
          Request ID <code>{receipt.requestId}</code>
        </span>
      </div>
    </div>
  );
}

export function approvalSeverity(score: number): Severity {
  if (score >= 0.85) return "critical";
  if (score >= 0.7) return "high";
  if (score >= 0.45) return "medium";
  return "low";
}

export function ProtectMutationError({ error }: { error: unknown }) {
  const requestId = error instanceof ApiError ? error.failure?.requestId : undefined;
  return (
    <div className="protect-error" role="alert">
      <ShieldAlert aria-hidden="true" size={15} />
      <span>
        {formatError(error)}
        {requestId ? (
          <>
            {" "}
            · Request ID <code>{requestId}</code>
          </>
        ) : null}
      </span>
    </div>
  );
}
