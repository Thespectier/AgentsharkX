import { useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  Cable,
  CheckCircle2,
  Copy,
  Network,
  Route,
  ServerCog,
  Waypoints,
} from "lucide-react";

import { currentSection, PageFrame, WorkspaceTabs } from "../../components/workspace";
import {
  Button,
  Card,
  CardHeader,
  DataTable,
  EmptyState,
  ErrorState,
  ExternalButton,
  PageHeader,
  PageSkeleton,
  PartialBanner,
  SourceBadge,
  StatusBadge,
  StatusOrb,
  type Column,
} from "../../components/ui";
import { formatCount } from "../../lib/format";
import { formatError, getScenario, requestEnvelope } from "../../lib/api";
import type { ConnectData, GatewayRoute, McpServer, Model, Provider } from "../../types";

const tabs = [
  { id: "overview", label: "Overview" },
  { id: "llm", label: "LLM" },
  { id: "mcp", label: "MCP" },
  { id: "traffic", label: "Traffic" },
  { id: "setup", label: "Setup" },
];

const routeColumns: Column<GatewayRoute>[] = [
  {
    key: "route",
    header: "Route",
    render: (item) => (
      <div className="primary-cell">
        <Route size={15} />
        <span>
          <strong>{item.id}</strong>
          <small>{item.hostname}</small>
        </span>
      </div>
    ),
  },
  { key: "protocol", header: "Protocol", render: (item) => <StatusBadge status={item.protocol} /> },
  { key: "listener", header: "Listener", render: (item) => <code>{item.listener}</code> },
  { key: "target", header: "Backend", render: (item) => item.target },
  { key: "requests", header: "Requests", render: (item) => formatCount(item.requests) },
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
  { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
];

export function ConnectPage() {
  const section = currentSection("connect", "overview");
  const scenario = getScenario();
  const query = useQuery({
    queryKey: ["connect", scenario],
    queryFn: ({ signal }) => requestEnvelope<ConnectData>("/api/v1/connect/summary", signal),
    retry: false,
  });
  if (query.isLoading) return <PageSkeleton label="Loading gateway resources" />;
  if (query.isError || !query.data)
    return (
      <PageFrame>
        <PageHeader
          description="agentgateway listeners, providers, models, MCP targets, and routes."
          eyebrow="Connect / agentgateway"
          title="Gateway data unavailable"
        />
        <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
      </PageFrame>
    );
  const { data, meta } = query.data;
  return (
    <PageFrame>
      <PageHeader
        actions={<ExternalButton href={data.consoleUrl}>Open in agentgateway</ExternalButton>}
        description="Verified agentgateway configuration and traffic surfaces. Advanced editing stays in the native console."
        eyebrow="Connect / agentgateway"
        title="Connect agents to every destination"
      >
        <WorkspaceTabs area="connect" items={tabs} />
      </PageHeader>
      <PartialBanner meta={meta} />
      {section === "overview" ? <ConnectOverview data={data} /> : null}
      {section === "llm" ? <LlmView data={data} /> : null}
      {section === "mcp" ? <McpView data={data} /> : null}
      {section === "traffic" ? <TrafficView data={data} /> : null}
      {section === "setup" ? <SetupView /> : null}
    </PageFrame>
  );
}

function ConnectOverview({ data }: { data: ConnectData }) {
  const icons = [Cable, Waypoints, Route, ServerCog];
  return (
    <>
      <div className="summary-grid">
        {data.summary.map((item, index) => {
          const Icon = icons[index];
          return (
            <Card className="summary-card" key={item.label}>
              <span className="summary-card__icon">
                <Icon size={18} />
              </span>
              <div>
                <p>{item.label}</p>
                <strong>{item.value}</strong>
                <span>
                  <CheckCircle2 size={12} /> {item.healthy} healthy
                </span>
              </div>
            </Card>
          );
        })}
      </div>
      {data.routes.length ? (
        <Card>
          <CardHeader
            action={<span className="fetched-at">Fetched 12:42:10 UTC</span>}
            description="Read-only normalized config dump; source and upstream IDs are preserved."
            title="Active traffic routes"
          />
          <DataTable columns={routeColumns} data={data.routes} label="Active gateway routes" />
        </Card>
      ) : (
        <EmptyState
          description="Connect an agentgateway config source to reveal listeners, gateways, routes, and backends."
          title="No gateway resources configured"
        />
      )}
    </>
  );
}

function LlmView({ data }: { data: ConnectData }) {
  const providerColumns: Column<Provider>[] = [
    {
      key: "name",
      header: "Provider",
      render: (item) => (
        <div className="primary-cell">
          <ServerCog size={15} />
          <span>
            <strong>{item.name}</strong>
            <small>{item.kind}</small>
          </span>
        </div>
      ),
    },
    { key: "models", header: "Models", render: (item) => item.models },
    { key: "requests", header: "Requests", render: (item) => formatCount(item.requests) },
    { key: "cost", header: "Est. cost", render: (item) => `$${item.cost.toFixed(2)}` },
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
    { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
  ];
  const modelColumns: Column<Model>[] = [
    {
      key: "model",
      header: "Model",
      render: (item) => (
        <div className="primary-cell">
          <Network size={15} />
          <span>
            <strong>{item.name}</strong>
            <small>{item.provider}</small>
          </span>
        </div>
      ),
    },
    { key: "input", header: "Input tokens", render: (item) => formatCount(item.inputTokens) },
    { key: "output", header: "Output tokens", render: (item) => formatCount(item.outputTokens) },
    { key: "latency", header: "P95 latency", render: (item) => `${item.p95LatencyMs} ms` },
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
  ];
  if (!data.providers.length)
    return (
      <EmptyState
        description="The pinned config contains no explicit LLM providers or models. AgentsharkX will not invent catalog entries."
        title="No LLM providers found"
      />
    );
  return (
    <div className="stack">
      <Card>
        <CardHeader
          description="Providers explicitly present in agentgateway configuration."
          title="Providers"
        />
        <DataTable columns={providerColumns} data={data.providers} label="LLM providers" />
      </Card>
      <Card>
        <CardHeader
          description="Usage values come from the mock request-log contract."
          title="Models"
        />
        <DataTable columns={modelColumns} data={data.models} label="LLM models" />
      </Card>
    </div>
  );
}

function McpView({ data }: { data: ConnectData }) {
  const columns: Column<McpServer>[] = [
    {
      key: "name",
      header: "MCP server",
      render: (item) => (
        <div className="primary-cell">
          <Waypoints size={15} />
          <span>
            <strong>{item.name}</strong>
            <small>{item.id}</small>
          </span>
        </div>
      ),
    },
    {
      key: "transport",
      header: "Transport",
      render: (item) => <StatusBadge status={item.transport} />,
    },
    { key: "tools", header: "Federated tools", render: (item) => item.tools },
    { key: "policy", header: "Policy", render: (item) => <code>{item.policy}</code> },
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
    { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
  ];
  return data.mcpServers.length ? (
    <Card>
      <CardHeader
        description="Gateway MCP targets remain distinct from AgentGuard MCP resources."
        title="MCP federation"
      />
      <DataTable columns={columns} data={data.mcpServers} label="Gateway MCP servers" />
    </Card>
  ) : (
    <EmptyState
      description="No MCP targets are explicitly present in the pinned agentgateway configuration."
      title="No MCP servers found"
    />
  );
}

function TrafficView({ data }: { data: ConnectData }) {
  return data.routes.length ? (
    <Card>
      <CardHeader
        description="HTTP, gRPC, and A2A routes from the verified gateway config surface."
        title="Listeners & routes"
      />
      <DataTable columns={routeColumns} data={data.routes} label="Traffic routes" />
    </Card>
  ) : (
    <EmptyState
      description="Create routes in agentgateway, then refresh this read-only view."
      title="No traffic routes found"
    />
  );
}

function SetupView() {
  return (
    <div className="setup-grid">
      <Card elevated>
        <CardHeader
          description="The BFF will read this URL server-side in Phase 2."
          title="Management endpoint"
        />
        <label className="field">
          <span>Base URL</span>
          <div className="copy-field">
            <code>http://agentgateway:15000</code>
            <Button aria-label="Copy gateway base URL" size="sm" variant="ghost">
              <Copy size={14} />
            </Button>
          </div>
        </label>
        <div className="connection-check">
          <StatusOrb status="healthy" />
          <div>
            <strong>Connection verified</strong>
            <span>Runtime v1.3.1 · 18 ms · standalone</span>
          </div>
        </div>
      </Card>
      <Card>
        <CardHeader
          description="Run from the AgentsharkX host after the upstream starts."
          title="Verification command"
        />
        <pre className="code-block">
          <code>curl -fsS http://127.0.0.1:15021/healthz/ready</code>
        </pre>
        <Button variant="secondary">
          Run check <ArrowRight size={14} />
        </Button>
        <p className="form-hint">
          Mock action in Phase 1. Real verification is implemented through the BFF in Phase 3.
        </p>
      </Card>
    </div>
  );
}
