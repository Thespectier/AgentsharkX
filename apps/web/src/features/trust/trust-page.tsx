import { useQuery } from "@tanstack/react-query";
import { FileCheck2, ShieldCheck } from "lucide-react";

import { PageFrame, useDocumentTitle, useWorkspaceSection } from "../../components/workspace";
import {
  Card,
  CardHeader,
  EmptyState,
  ErrorState,
  PageHeader,
  PageSkeleton,
  PartialBanner,
  StatusBadge,
} from "../../components/ui";
import type { ProtectSnapshot } from "../../generated/api-client";
import { formatCount } from "../../lib/format";
import { formatError, getScenario, requestOperation } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { GatewayGuardrailManager } from "../protect/gateway-guardrail-manager";
import { GatewayPolicyManager } from "../protect/gateway-policy-manager";
import { RuntimeRulesView } from "./runtime-rules";

export function TrustPage() {
  const section = useWorkspaceSection("trust", "overview");
  const heading = trustHeadings[section] ?? trustHeadings.overview;
  useDocumentTitle(heading.title);
  const query = useQuery({
    queryKey: ["trust-controls", getScenario()],
    queryFn: ({ signal }) => requestOperation("listPolicies", signal),
    retry: false,
  });

  if (query.isLoading) return <PageSkeleton label="Loading Trust controls" />;
  if (query.isError || !query.data) {
    return (
      <PageFrame>
        <PageHeader
          description="Guardrails, runtime rules, and policy controls remain independently sourced."
          eyebrow="Trust / Controls"
          title="Trust controls unavailable"
        />
        <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
      </PageFrame>
    );
  }

  const { data, meta } = query.data;
  return (
    <PageFrame>
      <PageHeader
        description={heading.description}
        eyebrow={`Trust / ${heading.label}`}
        title={heading.title}
      />
      <PartialBanner meta={meta} />
      {section === "overview" ? <TrustOverview data={data} /> : null}
      {section === "guardrails" ? <GatewayGuardrailManager /> : null}
      {section === "runtime-rules" ? <RuntimeRulesView data={data} /> : null}
      {section === "policy" ? <GatewayPolicyManager /> : null}
    </PageFrame>
  );
}

const trustHeadings: Record<string, { label: string; title: string; description: string }> = {
  overview: {
    label: "Overview",
    title: "Trust overview",
    description: "Review configured controls and the published rules that define trusted behavior.",
  },
  guardrails: {
    label: "Guardrails",
    title: "Guardrails",
    description: "Configure verified LLM, model, and MCP guardrail behavior.",
  },
  "runtime-rules": {
    label: "Runtime Rules",
    title: "Runtime Rules",
    description: "Check, publish, and remove explicit runtime rules for monitored Agents.",
  },
  policy: {
    label: "Policy",
    title: "Policy",
    description: "Manage verified LLM, model, and MCP policy configuration.",
  },
};

function TrustOverview({ data }: { data: ProtectSnapshot }) {
  const { t } = useI18n();
  const publishedRules = data.runtimeRules.filter((rule) => rule.status === "published").length;
  const configuredPolicies = data.gatewayPolicies.length;
  const recentControls = [
    ...data.runtimeRules.map((rule) => ({
      id: rule.id,
      name: rule.name,
      kind: "Runtime rule",
      scope: rule.scope,
      status: rule.status,
    })),
    ...data.gatewayPolicies.map((policy) => ({
      id: policy.id,
      name: policy.name,
      kind: policy.type,
      scope: policy.scope,
      status: policy.status,
    })),
  ];

  return (
    <div className="stack trust-overview">
      <div className="agent-monitoring__metrics">
        <Card as="article" className="agent-monitoring__metric">
          <span className="agent-monitoring__metric-icon">
            <FileCheck2 aria-hidden="true" size={18} />
          </span>
          <div>
            <p>{t("Configured policies")}</p>
            <strong>{formatCount(configuredPolicies)}</strong>
            <span>{t("Verified connection policy entries")}</span>
          </div>
        </Card>
        <Card as="article" className="agent-monitoring__metric">
          <span className="agent-monitoring__metric-icon agent-monitoring__metric-icon--active">
            <ShieldCheck aria-hidden="true" size={18} />
          </span>
          <div>
            <p>{t("Published runtime rules")}</p>
            <strong>{formatCount(publishedRules)}</strong>
            <span>{t("Explicitly reported published state")}</span>
          </div>
        </Card>
      </div>
      <Card>
        <CardHeader
          description="A compact view of controls currently reported through Agentshark."
          title="Control status"
        />
        {recentControls.length ? (
          <div className="trust-control-list">
            {recentControls.slice(0, 8).map((control) => (
              <article className="trust-control-row" key={control.id}>
                <span className="trust-control-row__icon">
                  <ShieldCheck aria-hidden="true" size={15} />
                </span>
                <div>
                  <strong>{control.name}</strong>
                  <span>
                    {t(control.kind)} · {control.scope}
                  </span>
                </div>
                <StatusBadge status={control.status} />
              </article>
            ))}
          </div>
        ) : (
          <EmptyState
            compact
            description="No policy or runtime rule has been reported yet."
            title="No Trust controls"
          />
        )}
      </Card>
    </div>
  );
}
