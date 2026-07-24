import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  CheckCircle2,
  ExternalLink,
  FileCode2,
  GitPullRequestArrow,
  LoaderCircle,
  ShieldAlert,
  ShieldCheck,
  Sparkles,
  Trash2,
} from "lucide-react";
import { useMemo, useRef, useState } from "react";

import { PageFrame, useWorkspaceSection } from "../../components/workspace";
import {
  Button,
  Card,
  CardHeader,
  DataTable,
  Dialog,
  EmptyState,
  ErrorState,
  ExternalButton,
  PageHeader,
  PageSkeleton,
  PartialBanner,
  SeverityBadge,
  SourceBadge,
  StatusBadge,
  type Column,
} from "../../components/ui";
import type {
  Approval,
  ApprovalPageEnvelope,
  ProtectMutationReceipt,
  ProtectPolicy,
  ProtectSnapshot,
  RuntimeRule,
  RuntimeRuleCheck,
} from "../../generated/api-client";
import {
  ApiError,
  formatError,
  getScenario,
  mutateOperation,
  requestOperation,
} from "../../lib/api";
import { synchronizeAgentGuardData } from "../../lib/query-sync";
import type { Severity } from "../../types";

type PolicyRow = {
  id: string;
  name: string;
  type: string;
  source: "agentgateway" | "agentguard";
  scope: string;
  phase: string;
  action: string;
  status: string;
};

const defaultRuntimeRuleSource = "RULE: review_external_delivery\nPOLICY: HUMAN_CHECK";

const policyColumns: Column<PolicyRow>[] = [
  {
    key: "policy",
    header: "Policy",
    render: (item) => (
      <div className="primary-cell">
        <ShieldCheck aria-hidden="true" size={15} />
        <span>
          <strong>{item.name}</strong>
          <small>{item.type}</small>
        </span>
      </div>
    ),
  },
  { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
  { key: "scope", header: "Scope", render: (item) => item.scope },
  { key: "phase", header: "Phase", render: (item) => <StatusBadge status={item.phase} /> },
  {
    key: "action",
    header: "Action",
    render: (item) => <strong className="decision-text">{item.action}</strong>,
  },
  { key: "status", header: "Status", render: (item) => <StatusBadge status={item.status} /> },
];

export function ProtectPage() {
  const section = useWorkspaceSection("protect", "policies");
  const scenario = getScenario();
  const query = useQuery({
    queryKey: ["protect", scenario],
    queryFn: ({ signal }) => requestOperation("listPolicies", signal),
    retry: false,
  });
  const approvals = useQuery({
    queryKey: ["protect-approvals", scenario],
    queryFn: ({ signal }) => requestOperation("listApprovals", { signal, query: { limit: 100 } }),
    retry: false,
  });
  if (query.isLoading) return <PageSkeleton label="Loading protection controls" />;
  if (query.isError || !query.data) {
    return (
      <PageFrame>
        <PageHeader
          description="Gateway policies, guardrails, runtime rules, plugins, and human approvals."
          eyebrow="Protect / Controls"
          title="Protection controls unavailable"
        />
        <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
      </PageFrame>
    );
  }
  const { data, meta } = query.data;
  return (
    <PageFrame>
      <PageHeader
        actions={
          <>
            {data.links.rawConfig ? (
              <ExternalButton href={data.links.rawConfig}>Configure agentgateway</ExternalButton>
            ) : null}
            {data.links.agentguardConsole ? (
              <ExternalButton href={data.links.agentguardConsole}>
                Configure AgentGuard
              </ExternalButton>
            ) : null}
          </>
        }
        description="Source, scope, phase, and action stay explicit; gateway and runtime policy models are never merged into a synthetic DSL."
        eyebrow="Protect / Policies & intervention"
        title="Enforce every critical boundary"
      />
      <PartialBanner meta={meta} />
      {section === "policies" ? <PolicyView data={data} /> : null}
      {section === "guardrails" ? <GuardrailView data={data} /> : null}
      {section === "runtime-rules" ? <RuntimeRulesView data={data} /> : null}
      {section === "plugins" ? <PluginsView data={data} /> : null}
      {section === "approvals" ? (
        <ApprovalsView
          envelope={approvals.data}
          error={approvals.error}
          loading={approvals.isLoading}
        />
      ) : null}
    </PageFrame>
  );
}

function gatewayRow(policy: ProtectPolicy): PolicyRow {
  return { ...policy };
}

function runtimeRow(rule: RuntimeRule): PolicyRow {
  return { ...rule, type: "Runtime Rule" };
}

function PolicyView({ data }: { data: ProtectSnapshot }) {
  const gateway = data.gatewayPolicies.map(gatewayRow);
  const runtime = data.runtimeRules.map(runtimeRow);
  if (!gateway.length && !runtime.length) {
    return (
      <EmptyState
        description="No gateway policies or AgentGuard rules are currently reported. The sources remain independently visible when one is unavailable."
        title="No policies reported"
      />
    );
  }
  return (
    <div className="stack">
      <Card>
        <CardHeader
          action={<SourceBadge source="agentgateway" />}
          description="Read-only summaries of exact keys in agentgateway route and backend configuration."
          title="Gateway controls"
        />
        {gateway.length ? (
          <DataTable columns={policyColumns} data={gateway} label="agentgateway policies" />
        ) : (
          <EmptyState
            compact
            description="No explicit gateway policies."
            title="No gateway controls"
          />
        )}
      </Card>
      <Card>
        <CardHeader
          action={<SourceBadge source="agentguard" />}
          description="Runtime rules retain AgentGuard action and phase semantics. Rule source is not returned."
          title="Runtime controls"
        />
        {runtime.length ? (
          <DataTable columns={policyColumns} data={runtime} label="AgentGuard runtime rules" />
        ) : (
          <EmptyState
            compact
            description="No explicit runtime rules."
            title="No runtime controls"
          />
        )}
      </Card>
    </div>
  );
}

function GuardrailView({ data }: { data: ProtectSnapshot }) {
  const guardrails = data.gatewayPolicies.filter((policy) => policy.type === "Content Guardrail");
  return (
    <div className="content-grid">
      <Card elevated>
        <CardHeader
          description="Only explicit request/response placement and configuration keys are shown."
          title="Content guardrails"
        />
        {guardrails.length ? (
          guardrails.map((item) => (
            <div className="policy-summary" key={item.id}>
              <span className="policy-summary__icon">
                <ShieldAlert aria-hidden="true" size={18} />
              </span>
              <div>
                <div>
                  <strong>{item.name}</strong>
                  <StatusBadge status={item.status} />
                </div>
                <p>
                  {item.scope} · {item.phase}
                </p>
                <footer>
                  <SourceBadge source={item.source} />
                  <span>{item.action}</span>
                </footer>
              </div>
            </div>
          ))
        ) : (
          <EmptyState
            compact
            description="No explicit content guardrail in gateway config."
            title="No guardrails"
          />
        )}
      </Card>
      <Card>
        <CardHeader
          description="Complex configuration remains in its owning control plane."
          title="Advanced configuration"
        />
        <div className="linkout-card">
          <FileCode2 aria-hidden="true" size={25} />
          <h2>Use the native policy editor</h2>
          <p>
            Raw config, CEL, credentials, and vendor-specific bodies are intentionally not
            duplicated.
          </p>
          {data.links.rawConfig ? (
            <a
              className="button button--secondary button--md"
              href={data.links.rawConfig}
              rel="noreferrer"
              target="_blank"
            >
              Open agentgateway <ExternalLink aria-hidden="true" size={14} />
            </a>
          ) : (
            <StatusBadge status="link unavailable" />
          )}
        </div>
      </Card>
    </div>
  );
}

function RuntimeRulesView({ data }: { data: ProtectSnapshot }) {
  const queryClient = useQueryClient();
  const agents = useMemo(() => {
    const values = new Map<string, string>();
    for (const plugin of data.plugins) values.set(plugin.agentId, plugin.agentUpstreamId);
    for (const rule of data.runtimeRules) {
      if (rule.agentId && rule.agentUpstreamId) values.set(rule.agentId, rule.agentUpstreamId);
    }
    return [...values.entries()];
  }, [data.plugins, data.runtimeRules]);
  const [composerOpen, setComposerOpen] = useState(false);
  const [source, setSource] = useState(defaultRuntimeRuleSource);
  const sourceRef = useRef(defaultRuntimeRuleSource);
  const [agentId, setAgentId] = useState(agents[0]?.[0] ?? "");
  const [note, setNote] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [checkResult, setCheckResult] = useState<RuntimeRuleCheck>();
  const [deleteRule, setDeleteRule] = useState<RuntimeRule>();
  const [deleteNote, setDeleteNote] = useState("");
  const [deleteConfirmed, setDeleteConfirmed] = useState(false);
  const [receipt, setReceipt] = useState<ProtectMutationReceipt>();

  const check = useMutation({
    mutationFn: (candidateSource: string) =>
      mutateOperation("checkRuntimeRule", { source: candidateSource }),
    onSuccess: (response, checkedSource) => {
      if (sourceRef.current === checkedSource) setCheckResult(response.data);
    },
  });
  const publish = useMutation({
    mutationFn: () =>
      mutateOperation(
        "publishRuntimeRule",
        { source, checkToken: checkResult?.checkToken ?? "", note, confirmed },
        { path: { agentId } },
      ),
    onSuccess: (response) => {
      setReceipt(response.data);
      setComposerOpen(false);
      setCheckResult(undefined);
      setSource(defaultRuntimeRuleSource);
      sourceRef.current = defaultRuntimeRuleSource;
      setNote("");
      setConfirmed(false);
      void synchronizeAgentGuardData(queryClient);
    },
  });
  const remove = useMutation({
    mutationFn: (rule: RuntimeRule) =>
      mutateOperation(
        "deleteRuntimeRule",
        { note: deleteNote, confirmed: deleteConfirmed },
        { path: { agentId: rule.agentId ?? "", ruleId: rule.id } },
      ),
    onSuccess: (response) => {
      setReceipt(response.data);
      setDeleteRule(undefined);
      setDeleteNote("");
      setDeleteConfirmed(false);
      void synchronizeAgentGuardData(queryClient);
    },
  });
  const rows = data.runtimeRules.map(runtimeRow);
  const columns: Column<PolicyRow>[] = [
    ...policyColumns,
    {
      key: "manage",
      header: "Manage",
      render: (item) => {
        const rule = data.runtimeRules.find((candidate) => candidate.id === item.id)!;
        return rule.userManaged && rule.agentId ? (
          <Button
            aria-label={`Delete ${rule.name}`}
            onClick={() => setDeleteRule(rule)}
            size="sm"
            variant="ghost"
          >
            <Trash2 aria-hidden="true" size={13} /> Delete
          </Button>
        ) : (
          <span className="resource-note">Read-only</span>
        );
      },
    },
  ];
  const publishReady = Boolean(
    checkResult?.publishable && checkResult.checkToken && agentId && note.trim() && confirmed,
  );
  const resetComposer = () => {
    setComposerOpen(false);
    setSource(defaultRuntimeRuleSource);
    sourceRef.current = defaultRuntimeRuleSource;
    setAgentId(agents[0]?.[0] ?? "");
    setNote("");
    setConfirmed(false);
    setCheckResult(undefined);
    check.reset();
    publish.reset();
  };
  const openComposer = () => {
    resetComposer();
    setComposerOpen(true);
  };
  return (
    <>
      <Card>
        <CardHeader
          action={
            <Button disabled={!agents.length} onClick={openComposer} variant="primary">
              New rule <Sparkles aria-hidden="true" size={14} />
            </Button>
          }
          description="A successful AgentGuard syntax check creates a short-lived, source-bound, one-use publish token."
          title="Runtime rules"
        />
        {receipt ? <MutationReceipt receipt={receipt} /> : null}
        {rows.length ? (
          <DataTable columns={columns} data={rows} label="AgentGuard runtime rules" />
        ) : (
          <EmptyState
            description="AgentGuard has not reported runtime rules."
            title="No runtime rules"
          />
        )}
      </Card>
      <Dialog
        description="Check exactly one rule, add an operator note, then explicitly confirm publication. Rule source is never written to audit logs."
        onClose={() => !publish.isPending && resetComposer()}
        open={composerOpen}
        size="wide"
        title="Publish runtime rule"
      >
        <div className="dialog-form protect-form">
          <label className="field">
            <span>Explicit AgentGuard agent</span>
            <select
              aria-label="Explicit AgentGuard agent"
              onChange={(event) => setAgentId(event.target.value)}
              value={agentId}
            >
              {agents.map(([id, upstream]) => (
                <option key={id} value={id}>
                  {upstream}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span>Rule source</span>
            <textarea
              aria-label="Rule source"
              onChange={(event) => {
                const nextSource = event.target.value;
                sourceRef.current = nextSource;
                setSource(nextSource);
                setCheckResult(undefined);
                check.reset();
                publish.reset();
              }}
              rows={5}
              value={source}
            />
          </label>
          <div className="protect-check-row">
            <Button
              disabled={check.isPending || !source.trim()}
              onClick={() => check.mutate(source)}
            >
              {check.isPending ? (
                <LoaderCircle className="spin" size={14} />
              ) : (
                <ShieldCheck size={14} />
              )}{" "}
              Check syntax
            </Button>
            {checkResult ? (
              <span
                className={
                  checkResult.publishable
                    ? "protect-check protect-check--ok"
                    : "protect-check protect-check--error"
                }
                role="status"
              >
                {checkResult.publishable
                  ? "Checked and publishable"
                  : (checkResult.errors[0]?.message ?? "Not publishable")}
              </span>
            ) : (
              <span className="resource-note">Check required before publish</span>
            )}
          </div>
          {check.isError ? <MutationError error={check.error} /> : null}
          <label className="field">
            <span>Operator note</span>
            <textarea
              aria-label="Operator note"
              onChange={(event) => setNote(event.target.value)}
              rows={2}
              value={note}
            />
          </label>
          <label className="confirm-field">
            <input
              checked={confirmed}
              onChange={(event) => setConfirmed(event.target.checked)}
              type="checkbox"
            />
            I confirm this checked rule should be published to the selected agent.
          </label>
          {publish.isError ? <MutationError error={publish.error} /> : null}
          <footer>
            <Button disabled={publish.isPending} onClick={resetComposer} variant="ghost">
              Cancel
            </Button>
            <Button
              disabled={!publishReady || publish.isPending}
              onClick={() => publish.mutate()}
              variant="primary"
            >
              {publish.isPending ? <LoaderCircle className="spin" size={14} /> : null} Publish
              checked rule
            </Button>
          </footer>
        </div>
      </Dialog>
      <Dialog
        description="Deletion is limited to a currently reported user-managed AgentGuard rule."
        onClose={() => !remove.isPending && setDeleteRule(undefined)}
        open={Boolean(deleteRule)}
        title={deleteRule ? `Delete ${deleteRule.name}` : "Delete runtime rule"}
      >
        <div className="dialog-form">
          <label className="field">
            <span>Operator note</span>
            <textarea
              aria-label="Deletion note"
              onChange={(event) => setDeleteNote(event.target.value)}
              rows={3}
              value={deleteNote}
            />
          </label>
          <label className="confirm-field">
            <input
              checked={deleteConfirmed}
              onChange={(event) => setDeleteConfirmed(event.target.checked)}
              type="checkbox"
            />
            I confirm this runtime rule should be deleted.
          </label>
          {remove.isError ? <MutationError error={remove.error} /> : null}
          <footer>
            <Button
              disabled={remove.isPending}
              onClick={() => setDeleteRule(undefined)}
              variant="ghost"
            >
              Cancel
            </Button>
            <Button
              disabled={!deleteNote.trim() || !deleteConfirmed || remove.isPending}
              onClick={() => deleteRule && remove.mutate(deleteRule)}
              variant="danger"
            >
              {remove.isPending ? <LoaderCircle className="spin" size={14} /> : null} Delete rule
            </Button>
          </footer>
        </div>
      </Dialog>
    </>
  );
}

function PluginsView({ data }: { data: ProtectSnapshot }) {
  if (!data.plugins.length) {
    return (
      <EmptyState
        description="No explicit per-agent plugin configuration is available."
        title="No plugin phases"
      />
    );
  }
  return (
    <div className="plugin-grid">
      {data.plugins.map((item) => {
        const active = item.enabledLocalPlugins.length + item.enabledRemotePlugins.length;
        const available = item.availableLocalPlugins.length + item.availableRemotePlugins.length;
        return (
          <Card as="article" className="plugin-card" key={item.id}>
            <span className="plugin-card__icon">
              <GitPullRequestArrow aria-hidden="true" size={19} />
            </span>
            <div>
              <StatusBadge status={active ? "active" : "disabled"} />
              <h2>{item.phase.replaceAll("_", " ")}</h2>
              <p>
                {active} of {available} available plugins enabled for {item.agentUpstreamId}.
              </p>
              <div className="coverage-bar">
                <span style={{ width: `${available ? (active / available) * 100 : 0}%` }} />
              </div>
              <footer>
                <SourceBadge source="agentguard" />
                <span>{item.configSource}</span>
              </footer>
            </div>
          </Card>
        );
      })}
    </div>
  );
}

function ApprovalsView({
  envelope,
  error,
  loading,
}: {
  envelope?: ApprovalPageEnvelope;
  error: Error | null;
  loading: boolean;
}) {
  const queryClient = useQueryClient();
  const [selected, setSelected] = useState<Approval>();
  const [note, setNote] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [decision, setDecision] = useState<"approve" | "deny">("approve");
  const [receipt, setReceipt] = useState<ProtectMutationReceipt>();
  const mutation = useMutation({
    mutationFn: ({ approval, action }: { approval: Approval; action: "approve" | "deny" }) =>
      mutateOperation(
        action === "approve" ? "approveTicket" : "denyTicket",
        { note, confirmed },
        { path: { ticketId: approval.id } },
      ),
    onSuccess: (response) => {
      setReceipt(response.data);
      setSelected(undefined);
      setNote("");
      setConfirmed(false);
      void synchronizeAgentGuardData(queryClient);
    },
    onError: (mutationError) => {
      if (mutationError instanceof ApiError && mutationError.status === 404) {
        void synchronizeAgentGuardData(queryClient);
      }
    },
  });
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
        {receipt ? <MutationReceipt receipt={receipt} /> : null}
        <EmptyState
          description="No AgentGuard tickets need an operator decision."
          title="Approval queue is clear"
        />
      </>
    );
  }
  const begin = (approval: Approval) => {
    mutation.reset();
    setReceipt(undefined);
    setNote("");
    setConfirmed(false);
    setDecision("approve");
    setSelected(approval);
  };
  const decide = (action: "approve" | "deny") => {
    if (!selected) return;
    setDecision(action);
    mutation.mutate({ approval: selected, action });
  };
  return (
    <>
      {receipt ? <MutationReceipt receipt={receipt} /> : null}
      <div className="approval-layout">
        <Card className="approval-list">
          <CardHeader
            description="Only sanitized context is shown; tool arguments and targets remain omitted."
            title="Pending review"
          />
          {approvals.map((approval) => (
            <button className="approval-item" key={approval.id} onClick={() => begin(approval)}>
              <span className="approval-item__risk">
                <SeverityBadge severity={approvalSeverity(approval.riskScore)} />
              </span>
              <div>
                <strong>{approval.tool || approval.eventType}</strong>
                <p>{approval.reason || "AgentGuard requested an operator decision."}</p>
                <footer>
                  <code>{approval.agentUpstreamId || "unknown agent"}</code>
                  <span>{approval.phase}</span>
                  <time>{new Date(approval.createdAt).toLocaleTimeString()}</time>
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
          <h2>Operator decisions stay explicit</h2>
          <p>
            Every decision requires a note and confirmation. A mutation is sent once; timeout
            recovery is always a deliberate manual retry.
          </p>
          <ul>
            <li>Source, runtime phase, rule matches, and risk remain visible.</li>
            <li>Duplicate clicks are disabled while a decision is pending.</li>
            <li>Receipts include the BFF request ID for audit lookup.</li>
          </ul>
        </Card>
      </div>
      <Dialog
        description="Review sanitized AgentGuard context, write a note, and explicitly confirm one decision."
        onClose={() => !mutation.isPending && setSelected(undefined)}
        open={Boolean(selected)}
        title={selected ? `Review ${selected.tool || selected.eventType}` : "Review approval"}
      >
        {selected ? (
          <div className="dialog-form">
            <div className="approval-dialog-summary">
              <SeverityBadge severity={approvalSeverity(selected.riskScore)} />
              <code>{selected.id}</code>
              <p>{selected.reason || "No upstream reason was provided."}</p>
              <SourceBadge source={selected.source} />
              <p>
                Phase: {selected.phase} · Matched rules:{" "}
                {selected.matchedRules.join(", ") || "none reported"}
              </p>
            </div>
            <label className="field">
              <span>Operator note</span>
              <textarea
                aria-label="Operator note"
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
            {mutation.isError ? <MutationError error={mutation.error} /> : null}
            <footer>
              <Button
                disabled={mutation.isPending}
                onClick={() => setSelected(undefined)}
                variant="ghost"
              >
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
        ) : null}
      </Dialog>
    </>
  );
}

function approvalSeverity(score: number): Severity {
  if (score >= 0.85) return "critical";
  if (score >= 0.7) return "high";
  if (score >= 0.45) return "medium";
  return "low";
}

function MutationReceipt({ receipt }: { receipt: ProtectMutationReceipt }) {
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

function MutationError({ error }: { error: unknown }) {
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
