import { useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  CheckCircle2,
  ExternalLink,
  FileCode2,
  GitPullRequestArrow,
  ShieldAlert,
  ShieldCheck,
  Sparkles,
} from "lucide-react";
import { useState } from "react";

import { currentSection, PageFrame, WorkspaceTabs } from "../../components/workspace";
import {
  Button,
  Card,
  CardHeader,
  DataTable,
  Dialog,
  EmptyState,
  ErrorState,
  PageHeader,
  PageSkeleton,
  PartialBanner,
  SeverityBadge,
  SourceBadge,
  StatusBadge,
  type Column,
} from "../../components/ui";
import { formatCount } from "../../lib/format";
import { formatError, getScenario, requestEnvelope } from "../../lib/api";
import type { Approval, Policy, ProtectData } from "../../types";

const tabs = [
  { id: "policies", label: "Policies" },
  { id: "guardrails", label: "Guardrails" },
  { id: "runtime-rules", label: "Runtime rules" },
  { id: "plugins", label: "Plugins" },
  { id: "approvals", label: "Approvals", badge: 3 },
];

export function ProtectPage() {
  const section = currentSection("protect", "policies");
  const scenario = getScenario();
  const query = useQuery({
    queryKey: ["protect", scenario],
    queryFn: ({ signal }) => requestEnvelope<ProtectData>("/api/v1/protect/policies", signal),
    retry: false,
  });
  if (query.isLoading) return <PageSkeleton label="Loading protection controls" />;
  if (query.isError || !query.data)
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
  const { data, meta } = query.data;
  const dynamicTabs = tabs.map((item) =>
    item.id === "approvals" ? { ...item, badge: data.approvals.length } : item,
  );
  return (
    <PageFrame>
      <PageHeader
        description="Keep source, scope, phase, and action visible. Gateway and runtime policy models remain separate."
        eyebrow="Protect / Policies & intervention"
        title="Enforce every critical boundary"
      >
        <WorkspaceTabs area="protect" items={dynamicTabs} />
      </PageHeader>
      <PartialBanner meta={meta} />
      {section === "policies" ? <PolicyView data={data} /> : null}
      {section === "guardrails" ? <GuardrailView data={data} /> : null}
      {section === "runtime-rules" ? <RuntimeRulesView data={data} /> : null}
      {section === "plugins" ? <PluginsView data={data} /> : null}
      {section === "approvals" ? <ApprovalsView approvals={data.approvals} /> : null}
    </PageFrame>
  );
}

const policyColumns: Column<Policy>[] = [
  {
    key: "policy",
    header: "Policy",
    render: (item) => (
      <div className="primary-cell">
        <ShieldCheck size={15} />
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
  { key: "matches", header: "24h matches", render: (item) => formatCount(item.matches24h) },
];

function PolicyView({ data }: { data: ProtectData }) {
  if (!data.policies.length)
    return (
      <EmptyState
        description="No gateway policies or AgentGuard rules are currently reported. These sources are intentionally not merged into one DSL."
        title="No policies reported"
      />
    );
  const gateway = data.policies.filter((policy) => policy.source === "agentgateway");
  const guard = data.policies.filter((policy) => policy.source === "agentguard");
  return (
    <div className="stack">
      <Card>
        <CardHeader
          action={<SourceBadge source="agentgateway" />}
          description="Read-only gateway authorization and content guardrail summaries."
          title="Gateway controls"
        />
        <DataTable columns={policyColumns} data={gateway} label="agentgateway policies" />
      </Card>
      <Card>
        <CardHeader
          action={<SourceBadge source="agentguard" />}
          description="Agent runtime rules retain their original phase and action semantics."
          title="Runtime controls"
        />
        <DataTable columns={policyColumns} data={guard} label="AgentGuard runtime rules" />
      </Card>
    </div>
  );
}

function GuardrailView({ data }: { data: ProtectData }) {
  const guardrails = data.policies.filter((policy) => policy.type === "Content Guardrail");
  return (
    <div className="content-grid">
      <Card elevated>
        <CardHeader
          description="Prompt and response protections reported by agentgateway config."
          title="Content guardrails"
        />
        {guardrails.length ? (
          guardrails.map((item) => (
            <div className="policy-summary" key={item.id}>
              <span className="policy-summary__icon">
                <ShieldAlert size={18} />
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
            description="No explicit content guardrail is present in gateway config."
            title="No guardrails"
          />
        )}
      </Card>
      <Card>
        <CardHeader
          description="Complex provider configuration remains with its owning control plane."
          title="Advanced configuration"
        />
        <div className="linkout-card">
          <FileCode2 size={25} />
          <h2>Use the native policy editor</h2>
          <p>
            Raw config, CEL, provider credentials, and vendor-specific guardrail options are
            intentionally not duplicated.
          </p>
          <a
            className="button button--secondary button--md"
            href="http://localhost:15000/ui"
            rel="noreferrer"
            target="_blank"
          >
            Open agentgateway <ExternalLink size={14} />
          </a>
        </div>
      </Card>
    </div>
  );
}

function RuntimeRulesView({ data }: { data: ProtectData }) {
  const rules = data.policies.filter((policy) => policy.type === "Runtime Rule");
  return (
    <Card>
      <CardHeader
        action={
          <Button disabled variant="primary">
            New rule <Sparkles size={14} />
          </Button>
        }
        description="Mock read-only preview. Check, publish, and delete mutations arrive in Phase 5."
        title="Runtime rules"
      />
      {rules.length ? (
        <DataTable columns={policyColumns} data={rules} label="AgentGuard runtime rules" />
      ) : (
        <EmptyState
          description="AgentGuard has not reported runtime rules."
          title="No runtime rules"
        />
      )}
    </Card>
  );
}

function PluginsView({ data }: { data: ProtectData }) {
  return (
    <div className="plugin-grid">
      {data.coverage.map((item) => (
        <Card as="article" className="plugin-card" key={item.phase}>
          <span className="plugin-card__icon">
            <GitPullRequestArrow size={19} />
          </span>
          <div>
            <StatusBadge status={item.active ? "active" : "disabled"} />
            <h2>{item.phase}</h2>
            <p>
              {item.active} of {item.available} available plugins enabled for this phase.
            </p>
            <div className="coverage-bar">
              <span style={{ width: `${(item.active / item.available) * 100}%` }} />
            </div>
            <footer>
              <SourceBadge source="agentguard" />
              <span>Per-agent config</span>
            </footer>
          </div>
        </Card>
      ))}
    </div>
  );
}

function ApprovalsView({ approvals }: { approvals: Approval[] }) {
  const [selected, setSelected] = useState<Approval | null>(null);
  const [result, setResult] = useState<string | null>(null);
  if (!approvals.length)
    return (
      <EmptyState
        description="No AgentGuard tickets need an operator decision."
        title="Approval queue is clear"
      />
    );
  return (
    <>
      <div className="approval-layout">
        <Card className="approval-list">
          <CardHeader
            description="Every decision requires context and a note in the real workflow."
            title="Pending review"
          />
          {approvals.map((approval) => (
            <button
              className="approval-item"
              key={approval.id}
              onClick={() => {
                setResult(null);
                setSelected(approval);
              }}
            >
              <span className="approval-item__risk">
                <SeverityBadge severity={approval.risk} />
              </span>
              <div>
                <strong>{approval.tool}</strong>
                <p>{approval.reason}</p>
                <footer>
                  <code>{approval.agentId}</code>
                  <span>{approval.phase}</span>
                  <time>{approval.requestedAt}</time>
                </footer>
              </div>
              <ArrowRight size={16} />
            </button>
          ))}
        </Card>
        <Card className="approval-context">
          <span className="approval-context__icon">
            <CheckCircle2 size={25} />
          </span>
          <h2>Operator decisions stay explicit</h2>
          <p>
            This Phase 1 queue demonstrates focus, confirmation, loading, and result surfaces
            without calling a write endpoint.
          </p>
          <ul>
            <li>Source and execution phase remain visible.</li>
            <li>Duplicate clicks are disabled during mutation.</li>
            <li>Sensitive payloads are not exposed.</li>
          </ul>
        </Card>
      </div>
      <Dialog
        description="This is a labelled Phase 1 mock. No AgentGuard mutation will be sent."
        onClose={() => setSelected(null)}
        open={Boolean(selected)}
        title={selected ? `Review ${selected.tool}` : "Review approval"}
      >
        {selected ? (
          <div className="dialog-form">
            <div className="approval-dialog-summary">
              <SeverityBadge severity={selected.risk} />
              <code>{selected.id}</code>
              <p>{selected.reason}</p>
              <SourceBadge source={selected.source} />
            </div>
            <label className="field">
              <span>Operator note</span>
              <textarea
                defaultValue="Reviewed against the active production change window."
                rows={3}
              />
            </label>
            {result ? (
              <div className="mock-result" role="status">
                <CheckCircle2 size={16} />
                {result}
              </div>
            ) : null}
            <footer>
              <Button onClick={() => setSelected(null)} variant="ghost">
                Cancel
              </Button>
              <Button onClick={() => setResult("Mock deny recorded locally")} variant="danger">
                Deny
              </Button>
              <Button onClick={() => setResult("Mock approval recorded locally")} variant="primary">
                Approve
              </Button>
            </footer>
          </div>
        ) : null}
      </Dialog>
    </>
  );
}
