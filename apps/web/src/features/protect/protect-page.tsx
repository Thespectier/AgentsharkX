import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouterState } from "@tanstack/react-router";
import { ArrowRight, BadgeCheck, Ban, CheckCircle2, ShieldCheck, ShieldX } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { PageFrame, useDocumentTitle, useWorkspaceSection } from "../../components/workspace";
import {
  Card,
  CardHeader,
  EmptyState,
  ErrorState,
  PageHeader,
  PageSkeleton,
  PartialBanner,
  SeverityBadge,
} from "../../components/ui";
import type {
  Approval,
  ApprovalPageEnvelope,
  AuditData,
  ProtectMutationReceipt,
  UnifiedEvent,
} from "../../generated/api-client";
import { formatCount, formatTimeWithZone } from "../../lib/format";
import { formatError, getScenario, requestOperation } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { synchronizeAgentGuardData } from "../../lib/query-sync";
import {
  ApprovalDecisionDialog,
  approvalSeverity,
  ProtectMutationReceiptNotice,
} from "./approval-decision";

export function ProtectPage() {
  const section = useWorkspaceSection("protect", "overview");
  const heading = protectHeadings[section] ?? protectHeadings.overview;
  useDocumentTitle(heading.title);
  const scenario = getScenario();
  const targetTicketId = useRouterState({
    select: (state) => new URLSearchParams(state.location.searchStr).get("ticketId") ?? undefined,
  });
  const audit = useQuery({
    queryKey: ["audit", scenario],
    queryFn: ({ signal }) => requestOperation("getAuditAnalytics", signal),
    enabled: section === "overview",
    retry: false,
  });
  const approvals = useQuery({
    queryKey: ["protect-approvals", scenario],
    queryFn: ({ signal }) => requestOperation("listApprovals", { signal, query: { limit: 100 } }),
    enabled: section === "approvals",
    retry: false,
  });

  const active = section === "approvals" ? approvals : audit;
  if (active.isLoading) return <PageSkeleton label="Loading protection decisions" />;
  if (active.isError || !active.data) {
    return (
      <PageFrame>
        <PageHeader
          description="Decision outcomes and approvals remain available only from explicit runtime evidence."
          eyebrow="Protect / Decisions"
          title="Protection data unavailable"
        />
        <ErrorState description={formatError(active.error)} onRetry={() => void active.refetch()} />
      </PageFrame>
    );
  }

  return (
    <PageFrame>
      <PageHeader
        description={heading.description}
        eyebrow={`Protect / ${heading.label}`}
        title={heading.title}
      />
      <PartialBanner meta={active.data.meta} />
      {section === "overview" && audit.data ? <ProtectOverview data={audit.data.data} /> : null}
      {section === "approvals" ? (
        <ApprovalsView
          envelope={approvals.data}
          error={approvals.error}
          loading={approvals.isLoading}
          targetTicketId={targetTicketId}
        />
      ) : null}
    </PageFrame>
  );
}

const protectHeadings: Record<string, { label: string; title: string; description: string }> = {
  overview: {
    label: "Overview",
    title: "Protection overview",
    description: "Review explicit passed, blocked, approved, and denied outcomes.",
  },
  approvals: {
    label: "Approvals",
    title: "Approvals",
    description: "Resolve protected actions that require an explicit administrator decision.",
  },
};

export type ProtectionStats = {
  passed: number;
  blocked: number;
  approved: number;
  denied: number;
};

export function protectionStats(events: UnifiedEvent[]): ProtectionStats {
  const stats: ProtectionStats = { passed: 0, blocked: 0, approved: 0, denied: 0 };
  for (const event of events) {
    const decision = String(event.decision ?? event.action ?? "")
      .trim()
      .toUpperCase();
    if (event.kind === "approval") {
      if (decision === "APPROVE") stats.approved += 1;
      if (decision === "DENY") stats.denied += 1;
      continue;
    }
    if (event.source !== "agentguard") continue;
    if (decision === "ALLOW") stats.passed += 1;
    if (decision === "DENY") stats.blocked += 1;
  }
  return stats;
}

function ProtectOverview({ data }: { data: AuditData }) {
  const { t } = useI18n();
  const stats = protectionStats(data.events);
  const items = [
    { label: "Passed", value: stats.passed, icon: CheckCircle2, tone: "passed" },
    { label: "Blocked", value: stats.blocked, icon: ShieldX, tone: "blocked" },
    { label: "Approved", value: stats.approved, icon: BadgeCheck, tone: "approved" },
    { label: "Denied", value: stats.denied, icon: Ban, tone: "denied" },
  ];
  return (
    <div className="stack protect-overview">
      <div className="protect-outcome-grid">
        {items.map((item) => {
          const Icon = item.icon;
          return (
            <Card
              as="article"
              className={`protect-outcome protect-outcome--${item.tone}`}
              key={item.label}
            >
              <span className="protect-outcome__icon">
                <Icon aria-hidden="true" size={19} />
              </span>
              <div>
                <p>{t(item.label)}</p>
                <strong>{formatCount(item.value)}</strong>
                <span>{t("Explicit events in the current Audit snapshot")}</span>
              </div>
            </Card>
          );
        })}
      </div>
      <Card>
        <CardHeader
          description="Only explicit ALLOW and DENY decisions plus completed approval outcomes are counted. Pending and indeterminate actions are excluded."
          title="Decision coverage"
        />
        <div className="protect-coverage">
          <ShieldCheck aria-hidden="true" size={20} />
          <div>
            <strong>
              {t("{count} explicit outcomes", {
                count: Object.values(stats).reduce((sum, value) => sum + value, 0),
              })}
            </strong>
            <span>
              {t("The current API does not provide a dedicated all-time protection aggregate.")}
            </span>
          </div>
        </div>
      </Card>
    </div>
  );
}

export function ApprovalsView({
  envelope,
  error,
  loading,
  targetTicketId,
}: {
  envelope?: ApprovalPageEnvelope;
  error: Error | null;
  loading: boolean;
  targetTicketId?: string;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [selected, setSelected] = useState<Approval>();
  const [receipt, setReceipt] = useState<ProtectMutationReceipt>();
  const autoOpenedTicket = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (!targetTicketId) {
      autoOpenedTicket.current = undefined;
      return;
    }
    if (autoOpenedTicket.current === targetTicketId) return;
    const approval = approvalForTicketId(envelope?.data.items ?? [], targetTicketId);
    if (!approval) return;
    autoOpenedTicket.current = targetTicketId;
    setReceipt(undefined);
    setSelected(approval);
  }, [envelope, targetTicketId]);

  if (loading) return <PageSkeleton label="Loading approval queue" />;
  if (error || !envelope)
    return (
      <ErrorState
        description={formatError(error)}
        onRetry={() => void synchronizeAgentGuardData(queryClient)}
      />
    );
  const approvals = envelope.data.items;
  if (!approvals.length) {
    return (
      <>
        {receipt ? <ProtectMutationReceiptNotice receipt={receipt} /> : null}
        <EmptyState
          description="No pending ticket needs an administrator decision."
          title="Approval queue is clear"
        />
      </>
    );
  }
  const begin = (approval: Approval) => {
    setReceipt(undefined);
    setSelected(approval);
  };
  return (
    <>
      {receipt ? <ProtectMutationReceiptNotice receipt={receipt} /> : null}
      <div className="approval-layout">
        <Card className="approval-list">
          <CardHeader
            description="Review the reported Agent, runtime phase, risk, and reason before deciding."
            title="Pending review"
          />
          {approvals.map((approval) => (
            <button
              aria-current={selected?.id === approval.id ? "true" : undefined}
              className="approval-item"
              key={approval.id}
              onClick={() => begin(approval)}
            >
              <span className="approval-item__risk">
                <SeverityBadge severity={approvalSeverity(approval.riskScore)} />
              </span>
              <div>
                <strong>{approval.tool || approval.eventType}</strong>
                <p>{approval.reason || "A protected action requires an administrator decision."}</p>
                <footer>
                  <code>{approval.agentUpstreamId || "unknown agent"}</code>
                  <span>{approval.phase}</span>
                  <time>{formatTimeWithZone(approval.createdAt)}</time>
                </footer>
              </div>
              <ArrowRight aria-hidden="true" size={16} />
            </button>
          ))}
        </Card>
        <Card className="approval-context">
          <span className="approval-context__icon">
            <CheckCircle2 aria-hidden="true" size={25} />
          </span>
          <h2>{t("Administrator decisions stay explicit")}</h2>
          <p>
            {t(
              "Every decision requires a note and confirmation. A mutation is sent once; timeout recovery is always a deliberate manual retry.",
            )}
          </p>
        </Card>
      </div>
      {selected ? (
        <ApprovalDecisionDialog
          approval={selected}
          onClose={() => setSelected(undefined)}
          onReceipt={setReceipt}
        />
      ) : null}
    </>
  );
}

export function approvalForTicketId(
  approvals: Approval[],
  ticketId?: string,
): Approval | undefined {
  if (!ticketId) return undefined;
  return approvals.find((approval) => approval.id === ticketId);
}
