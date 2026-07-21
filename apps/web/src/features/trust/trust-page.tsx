import { useQuery } from "@tanstack/react-query";
import { Bot, Boxes, ScanSearch, ShieldQuestion, UserRoundCheck } from "lucide-react";

import { currentSection, PageFrame, WorkspaceTabs } from "../../components/workspace";
import {
  Card,
  CardHeader,
  DataTable,
  EmptyState,
  ErrorState,
  PageHeader,
  PageSkeleton,
  PartialBanner,
  SourceBadge,
  StatusBadge,
  StatusOrb,
  type Column,
} from "../../components/ui";
import { formatError, getScenario, requestEnvelope } from "../../lib/api";
import type { AgentIdentity, TrustData, TrustResource } from "../../types";

const tabs = [
  { id: "agents", label: "Agents" },
  { id: "resources", label: "Resources" },
  { id: "scans", label: "Scans" },
];

export function TrustPage() {
  const section = currentSection("trust", "agents");
  const scenario = getScenario();
  const query = useQuery({
    queryKey: ["trust", scenario],
    queryFn: ({ signal }) => requestEnvelope<TrustData>("/api/v1/trust/resources", signal),
    retry: false,
  });
  if (query.isLoading) return <PageSkeleton label="Loading trusted context" />;
  if (query.isError || !query.data)
    return (
      <PageFrame>
        <PageHeader
          description="Explicit AgentGuard identities, tools, skills, MCP resources, labels, and scan results."
          eyebrow="Trust / AgentGuard"
          title="Trusted context unavailable"
        />
        <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
      </PageFrame>
    );
  const { data, meta } = query.data;
  return (
    <PageFrame>
      <PageHeader
        description="Inspect identities and resources reported explicitly by AgentGuard. Unknown values stay unknown."
        eyebrow="Trust / AgentGuard context"
        title="Know what every agent can reach"
      >
        <WorkspaceTabs area="trust" items={tabs} />
      </PageHeader>
      <PartialBanner meta={meta} />
      {section === "agents" ? <AgentsView data={data} /> : null}
      {section === "resources" ? <ResourcesView data={data} /> : null}
      {section === "scans" ? <ScansView data={data} /> : null}
    </PageFrame>
  );
}

function AgentsView({ data }: { data: TrustData }) {
  const columns: Column<AgentIdentity>[] = [
    {
      key: "agent",
      header: "Agent",
      render: (item) => (
        <div className="primary-cell">
          <Bot size={15} />
          <span>
            <strong>{item.name}</strong>
            <small>{item.id}</small>
          </span>
        </div>
      ),
    },
    {
      key: "status",
      header: "Status",
      render: (item) => (
        <span className="status-cell">
          <StatusOrb status={item.status} />
          {item.status}
        </span>
      ),
    },
    { key: "framework", header: "Framework", render: (item) => item.framework },
    { key: "principal", header: "Principal", render: (item) => <code>{item.principal}</code> },
    {
      key: "trust",
      header: "Trust level",
      render: (item) => <StatusBadge status={item.trustLevel} />,
    },
    { key: "sessions", header: "Sessions", render: (item) => item.sessions },
    { key: "activity", header: "Last active", render: (item) => item.lastActive },
    { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
  ];
  if (!data.agents.length)
    return (
      <EmptyState
        description="AgentGuard has not reported an explicit agent_id through resources or sessions. Gateway clients are not promoted into identities."
        title="No explicit agents reported"
      />
    );
  return (
    <div className="trust-layout">
      <Card className="trust-distribution">
        <CardHeader description="Only explicit AgentGuard trust values." title="Trust posture" />
        <div className="distribution-list">
          {data.trustDistribution.map((item) => (
            <div key={item.name}>
              <span>
                <i className={`trust-dot trust-dot--${item.name.toLowerCase()}`} />
                {item.name}
              </span>
              <strong>{item.value}</strong>
              <div>
                <span style={{ width: `${Math.max(item.value * 4, 5)}%` }} />
              </div>
            </div>
          ))}
        </div>
        <div className="trust-note">
          <ShieldQuestion size={17} />
          <p>
            This is contextual trust metadata, not remote attestation or cryptographic identity.
          </p>
        </div>
      </Card>
      <Card className="trust-table">
        <CardHeader
          description="Identity fields retain AgentGuard as their source."
          title="Observed agents"
        />
        <DataTable columns={columns} data={data.agents} label="Observed AgentGuard agents" />
      </Card>
    </div>
  );
}

function ResourcesView({ data }: { data: TrustData }) {
  const columns: Column<TrustResource>[] = [
    {
      key: "resource",
      header: "Resource",
      render: (item) => (
        <div className="primary-cell">
          <Boxes size={15} />
          <span>
            <strong>{item.name}</strong>
            <small>{item.id}</small>
          </span>
        </div>
      ),
    },
    { key: "type", header: "Type", render: (item) => <StatusBadge status={item.type} /> },
    { key: "agent", header: "Agent", render: (item) => <code>{item.ownerAgent}</code> },
    { key: "boundary", header: "Boundary", render: (item) => item.boundary },
    {
      key: "sensitivity",
      header: "Sensitivity",
      render: (item) => <StatusBadge status={item.sensitivity} />,
    },
    { key: "integrity", header: "Integrity", render: (item) => item.integrity },
    { key: "scan", header: "Scan", render: (item) => <StatusBadge status={item.scanStatus} /> },
    { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
  ];
  return data.resources.length ? (
    <Card>
      <CardHeader
        description="Tools, Skills, and MCP resources share a table but keep their original type."
        title="Runtime resources"
      />
      <DataTable columns={columns} data={data.resources} label="AgentGuard runtime resources" />
    </Card>
  ) : (
    <EmptyState
      description="No Tools, Skills, or MCP resources have been reported by AgentGuard."
      title="No resources reported"
    />
  );
}

function ScansView({ data }: { data: TrustData }) {
  return data.scans.length ? (
    <div className="scan-grid">
      {data.scans.map((scan) => (
        <Card as="article" className="scan-card" key={scan.id}>
          <span className="scan-card__icon">
            <ScanSearch size={19} />
          </span>
          <div>
            <div className="scan-card__top">
              <StatusBadge status={scan.type} />
              <StatusBadge status={scan.status} />
            </div>
            <h2>{scan.resource}</h2>
            <p>
              {scan.findings
                ? `${scan.findings} findings require review`
                : scan.status === "running"
                  ? "Detector is processing the submitted resource"
                  : "No findings in the latest scan"}
            </p>
            <footer>
              <span>{scan.id}</span>
              <time>{scan.updatedAt}</time>
            </footer>
          </div>
        </Card>
      ))}
    </div>
  ) : (
    <EmptyState
      description="Trigger a Skill or MCP scan after AgentGuard reports a resource."
      title="No scans available"
    />
  );
}
