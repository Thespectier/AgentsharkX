import { useQuery } from "@tanstack/react-query";
import {
  Activity,
  Cable,
  CheckCircle2,
  Network,
  RefreshCw,
  Route,
  ServerCog,
  Waypoints,
} from "lucide-react";
import { useRef, useState } from "react";

import { PageFrame, useWorkspaceSection, WorkspaceTabs } from "../../components/workspace";
import {
  Button,
  Card,
  CardHeader,
  DataTable,
  DefinitionList,
  DetailDrawer,
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
import type {
  ConnectSummary,
  GatewayMCPServer,
  GatewayModel,
  GatewayProvider,
  GatewayRoute,
} from "../../generated/api-client";
import { formatCount, formatTime } from "../../lib/format";
import { formatError, getScenario, requestOperation } from "../../lib/api";

const tabs = [
  { id: "overview", label: "Overview" },
  { id: "llm", label: "LLM" },
  { id: "mcp", label: "MCP" },
  { id: "traffic", label: "Traffic" },
  { id: "setup", label: "Setup" },
];

type Selection =
  | { kind: "provider"; id: string }
  | { kind: "model"; id: string }
  | { kind: "mcp"; id: string }
  | { kind: "route"; id: string };

export function ConnectPage() {
  const section = useWorkspaceSection("connect", "overview");
  const scenario = getScenario();
  const [selection, setSelection] = useState<Selection>();
  const triggerRef = useRef<HTMLElement | null>(null);
  const summary = useQuery({
    queryKey: ["connect-summary", scenario],
    queryFn: ({ signal }) => requestOperation("getConnectSummary", signal),
    retry: false,
  });
  if (summary.isLoading) return <PageSkeleton label="Loading gateway resources" />;
  if (summary.isError || !summary.data)
    return (
      <PageFrame>
        <PageHeader
          description="agentgateway listeners, providers, models, MCP targets, and routes."
          eyebrow="Connect / agentgateway"
          title="Gateway data unavailable"
        />
        <ErrorState
          description={formatError(summary.error)}
          onRetry={() => void summary.refetch()}
        />
      </PageFrame>
    );
  const { data, meta } = summary.data;
  const open = (next: Selection, trigger: HTMLTableRowElement) => {
    triggerRef.current = trigger;
    setSelection(next);
  };
  return (
    <PageFrame>
      <PageHeader
        actions={
          data.links.rawConfig || data.links.console ? (
            <ExternalButton href={data.links.rawConfig ?? data.links.console!}>
              Configure agentgateway
            </ExternalButton>
          ) : undefined
        }
        description="Verified agentgateway configuration and traffic surfaces. Advanced editing stays in the native console."
        eyebrow="Connect / agentgateway"
        title="Connect agents to every destination"
      >
        <WorkspaceTabs area="connect" items={tabs} />
      </PageHeader>
      <PartialBanner meta={meta} />
      {section === "overview" ? (
        <ConnectOverview summary={data} fetchedAt={meta.fetchedAt} />
      ) : null}
      {section === "llm" ? <LlmView onOpen={open} /> : null}
      {section === "mcp" ? <McpView onOpen={open} /> : null}
      {section === "traffic" ? <TrafficView onOpen={open} /> : null}
      {section === "setup" ? <SetupView /> : null}
      <ResourceDetail
        selection={selection}
        onClose={() => setSelection(undefined)}
        returnFocusRef={triggerRef}
      />
    </PageFrame>
  );
}

function ConnectOverview({ summary, fetchedAt }: { summary: ConnectSummary; fetchedAt: string }) {
  const icons = [Cable, Route, Network, Waypoints];
  const analytics = summary.analytics;
  return (
    <>
      <div className="summary-grid">
        {summary.counts.map((item, index) => {
          const Icon = icons[index] ?? Cable;
          return (
            <Card className="summary-card" key={item.id}>
              <span className="summary-card__icon">
                <Icon size={18} />
              </span>
              <div>
                <p>{item.label}</p>
                <strong>{item.value === null ? "Unavailable" : formatCount(item.value)}</strong>
                <span>
                  <CheckCircle2 size={12} /> {item.status}
                </span>
              </div>
            </Card>
          );
        })}
      </div>
      <div className="content-grid">
        <Card>
          <CardHeader
            action={<span className="fetched-at">Fetched {formatTime(fetchedAt)} UTC</span>}
            description="Runtime and configuration were checked independently by the BFF."
            title="Connection status"
          />
          <div className="connection-check">
            <StatusOrb status={summary.health.status} />
            <div>
              <strong>{summary.health.status}</strong>
              <span>
                {summary.health.version ?? "Version unavailable"} ·{" "}
                {summary.health.latencyMs ?? "—"} ms
              </span>
            </div>
          </div>
        </Card>
        <Card>
          <CardHeader
            action={<StatusBadge status={analytics.status} />}
            description="Derived only from the verified request-log analytics contract."
            title="Analytics summary"
          />
          {analytics.status === "available" ? (
            <DefinitionList
              items={[
                { label: "Requests", value: formatNullable(analytics.requests) },
                { label: "Total tokens", value: formatNullable(analytics.totalTokens) },
                {
                  label: "Estimated cost",
                  value: analytics.cost === null ? "Not provided" : `$${analytics.cost.toFixed(4)}`,
                },
                { label: "Buckets", value: analytics.buckets.length },
              ]}
            />
          ) : (
            <p className="resource-note">{analytics.reason ?? "Analytics is unavailable."}</p>
          )}
        </Card>
      </div>
      <NativeLinks links={summary.links} />
    </>
  );
}

function LlmView({
  onOpen,
}: {
  onOpen: (selection: Selection, trigger: HTMLTableRowElement) => void;
}) {
  return (
    <div className="stack">
      <ProviderTable onOpen={onOpen} />
      <ModelTable onOpen={onOpen} />
    </div>
  );
}

function ProviderTable({
  onOpen,
}: {
  onOpen: (selection: Selection, trigger: HTMLTableRowElement) => void;
}) {
  const pager = usePager();
  const query = useQuery({
    queryKey: ["connect-providers", pager.search, pager.cursor, getScenario()],
    queryFn: ({ signal }) =>
      requestOperation("listProviders", {
        signal,
        query: { q: pager.search, cursor: pager.cursor, limit: 10 },
      }),
    retry: false,
  });
  const columns: Column<GatewayProvider>[] = [
    {
      key: "name",
      header: "Provider",
      render: (item) => (
        <Primary
          icon={ServerCog}
          title={item.name}
          subtitle={item.upstreamId ?? "No upstream ID"}
        />
      ),
    },
    { key: "kind", header: "Kind", render: (item) => <StatusBadge status={item.kind} /> },
    { key: "models", header: "Explicit references", render: (item) => item.modelCount },
    { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
    { key: "fetched", header: "Fetched", render: (item) => `${formatTime(item.fetchedAt)} UTC` },
  ];
  return (
    <ResourceCard
      title="Providers"
      description="Providers explicitly present in agentgateway configuration."
      query={query}
      pager={pager}
      render={(page) => (
        <DataTable
          columns={columns}
          data={page.items}
          label="LLM providers"
          onRowClick={(item, trigger) => onOpen({ kind: "provider", id: item.id }, trigger)}
        />
      )}
    />
  );
}

function ModelTable({
  onOpen,
}: {
  onOpen: (selection: Selection, trigger: HTMLTableRowElement) => void;
}) {
  const pager = usePager();
  const query = useQuery({
    queryKey: ["connect-models", pager.search, pager.cursor, getScenario()],
    queryFn: ({ signal }) =>
      requestOperation("listModels", {
        signal,
        query: { q: pager.search, cursor: pager.cursor, limit: 10 },
      }),
    retry: false,
  });
  const columns: Column<GatewayModel>[] = [
    {
      key: "name",
      header: "Model",
      render: (item) => (
        <Primary icon={Network} title={item.name} subtitle={item.upstreamId ?? "No upstream ID"} />
      ),
    },
    { key: "kind", header: "Kind", render: (item) => <StatusBadge status={item.kind} /> },
    {
      key: "provider",
      header: "Provider / routing",
      render: (item) => <code>{item.provider ?? item.routing ?? "Not provided"}</code>,
    },
    { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
    { key: "fetched", header: "Fetched", render: (item) => `${formatTime(item.fetchedAt)} UTC` },
  ];
  return (
    <ResourceCard
      title="Models"
      description="Direct and virtual models remain explicitly distinguished."
      query={query}
      pager={pager}
      render={(page) => (
        <DataTable
          columns={columns}
          data={page.items}
          label="LLM models"
          onRowClick={(item, trigger) => onOpen({ kind: "model", id: item.id }, trigger)}
        />
      )}
    />
  );
}

function McpView({
  onOpen,
}: {
  onOpen: (selection: Selection, trigger: HTMLTableRowElement) => void;
}) {
  const pager = usePager();
  const query = useQuery({
    queryKey: ["connect-mcp", pager.search, pager.cursor, getScenario()],
    queryFn: ({ signal }) =>
      requestOperation("listGatewayMcpServers", {
        signal,
        query: { q: pager.search, cursor: pager.cursor, limit: 10 },
      }),
    retry: false,
  });
  const columns: Column<GatewayMCPServer>[] = [
    {
      key: "name",
      header: "MCP server",
      render: (item) => (
        <Primary
          icon={Waypoints}
          title={item.name}
          subtitle={item.upstreamId ?? "No upstream ID"}
        />
      ),
    },
    {
      key: "transport",
      header: "Transport",
      render: (item) => <StatusBadge status={item.transport} />,
    },
    { key: "scope", header: "Scope", render: (item) => <code>{item.scope}</code> },
    { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
    { key: "fetched", header: "Fetched", render: (item) => `${formatTime(item.fetchedAt)} UTC` },
  ];
  return (
    <ResourceCard
      title="MCP federation"
      description="Gateway MCP targets remain distinct from AgentGuard resources."
      query={query}
      pager={pager}
      render={(page) => (
        <DataTable
          columns={columns}
          data={page.items}
          label="Gateway MCP servers"
          onRowClick={(item, trigger) => onOpen({ kind: "mcp", id: item.id }, trigger)}
        />
      )}
    />
  );
}

function TrafficView({
  onOpen,
}: {
  onOpen: (selection: Selection, trigger: HTMLTableRowElement) => void;
}) {
  const pager = usePager();
  const query = useQuery({
    queryKey: ["connect-routes", pager.search, pager.cursor, getScenario()],
    queryFn: ({ signal }) =>
      requestOperation("listTrafficRoutes", {
        signal,
        query: { q: pager.search, cursor: pager.cursor, limit: 10 },
      }),
    retry: false,
  });
  const columns: Column<GatewayRoute>[] = [
    {
      key: "name",
      header: "Route",
      render: (item) => (
        <Primary
          icon={Route}
          title={item.name}
          subtitle={item.hostnames.join(", ") || "No hostname"}
        />
      ),
    },
    {
      key: "protocol",
      header: "Protocol",
      render: (item) => <StatusBadge status={item.protocol} />,
    },
    {
      key: "listener",
      header: "Listener",
      render: (item) => (
        <code>
          {item.listener}:{item.port}
        </code>
      ),
    },
    { key: "target", header: "Backends", render: (item) => targetSummary(item) },
    { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
    { key: "fetched", header: "Fetched", render: (item) => `${formatTime(item.fetchedAt)} UTC` },
  ];
  return (
    <ResourceCard
      title="Listeners & routes"
      description="HTTP and TCP routes from explicit configuration fields."
      query={query}
      pager={pager}
      render={(page) => (
        <DataTable
          columns={columns}
          data={page.items}
          label="Traffic routes"
          onRowClick={(item, trigger) => onOpen({ kind: "route", id: item.id }, trigger)}
        />
      )}
    />
  );
}

type Pager = ReturnType<typeof usePager>;

function ResourceCard<T extends { id: string }>({
  title,
  description,
  query,
  pager,
  render,
}: {
  title: string;
  description: string;
  query: {
    isLoading: boolean;
    isFetching: boolean;
    isError: boolean;
    error: unknown;
    data?: { data: { items: T[]; nextCursor: string | null; total: number } };
    refetch: () => unknown;
  };
  pager: Pager;
  render: (page: { items: T[]; nextCursor: string | null; total: number }) => React.ReactNode;
}) {
  const page = query.data?.data;
  return (
    <Card>
      <CardHeader description={description} title={title} />
      <ResourceControls pager={pager} page={page} fetching={query.isFetching} />
      {query.isLoading ? <div className="resource-note">Loading explicit resources…</div> : null}
      {query.isError ? (
        <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
      ) : null}
      {page && page.items.length ? render(page) : null}
      {page && !page.items.length ? (
        <EmptyState
          description="No explicit upstream resources match this query."
          title={`No ${title.toLowerCase()} found`}
        />
      ) : null}
    </Card>
  );
}

function ResourceControls({
  pager,
  page,
  fetching,
}: {
  pager: Pager;
  page?: { nextCursor: string | null; total: number };
  fetching: boolean;
}) {
  return (
    <div className="resource-toolbar">
      <label>
        <span className="sr-only">Filter resources</span>
        <input
          placeholder="Filter explicit resources"
          value={pager.search}
          onChange={(event) => pager.setSearch(event.target.value)}
        />
      </label>
      <span>{page ? `${page.total} total` : "—"}</span>
      <Button
        disabled={!pager.canPrevious || fetching}
        onClick={pager.previous}
        size="sm"
        variant="ghost"
      >
        Previous
      </Button>
      <Button
        disabled={!page?.nextCursor || fetching}
        onClick={() => page?.nextCursor && pager.next(page.nextCursor)}
        size="sm"
        variant="ghost"
      >
        Next
      </Button>
    </div>
  );
}

function usePager() {
  const [search, updateSearch] = useState("");
  const [cursor, setCursor] = useState("");
  const [history, setHistory] = useState<string[]>([]);
  return {
    search,
    cursor,
    canPrevious: history.length > 0,
    setSearch(value: string) {
      updateSearch(value);
      setCursor("");
      setHistory([]);
    },
    next(value: string) {
      setHistory((items) => [...items, cursor]);
      setCursor(value);
    },
    previous() {
      setHistory((items) => {
        const next = [...items];
        setCursor(next.pop() ?? "");
        return next;
      });
    },
  };
}

function SetupView() {
  const query = useQuery({
    queryKey: ["connect-setup", getScenario()],
    queryFn: ({ signal }) => requestOperation("verifyGatewaySetup", signal),
    retry: false,
  });
  if (query.isLoading) return <PageSkeleton label="Verifying agentgateway management access" />;
  if (query.isError || !query.data)
    return (
      <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
    );
  const setup = query.data.data;
  return (
    <div className="setup-grid">
      <Card elevated>
        <CardHeader
          description="Live BFF checks against /api/runtime and /api/config."
          title="Management verification"
        />
        <div className="connection-check">
          <StatusOrb status={setup.status} />
          <div>
            <strong>
              {setup.configurationReadable ? "Connection verified" : "Configuration unreadable"}
            </strong>
            <span>
              {setup.version ?? "Version unavailable"} · {setup.latencyMs ?? "—"} ms · checked{" "}
              {formatTime(setup.checkedAt)} UTC
            </span>
          </div>
        </div>
        {setup.message ? <p className="resource-note">{setup.message}</p> : null}
        <Button onClick={() => void query.refetch()} variant="secondary">
          <RefreshCw size={14} /> Run check
        </Button>
      </Card>
      <Card>
        <CardHeader
          description="Advanced editors stay in the pinned agentgateway console."
          title="Native console tools"
        />
        <NativeLinks links={setup.links} compact />
      </Card>
    </div>
  );
}

function NativeLinks({
  links,
  compact = false,
}: {
  links: { rawConfig?: string; cel?: string; llmPlayground?: string; mcpPlayground?: string };
  compact?: boolean;
}) {
  const values = [
    ["Raw Config", links.rawConfig],
    ["CEL Playground", links.cel],
    ["LLM Playground", links.llmPlayground],
    ["MCP Playground", links.mcpPlayground],
  ] as const;
  const available = values.flatMap(([label, href]) => (href ? [{ label, href }] : []));
  if (!available.length)
    return compact ? (
      <p className="resource-note">No validated console URL is configured.</p>
    ) : null;
  return (
    <Card className={compact ? "native-links native-links--compact" : "native-links"}>
      {!compact ? (
        <CardHeader
          description="Use upstream-native tools for advanced editing and testing."
          title="Open in agentgateway"
        />
      ) : null}
      <div className="native-links__actions">
        {available.map(({ label, href }) => (
          <ExternalButton href={href} key={label}>
            {label}
          </ExternalButton>
        ))}
      </div>
    </Card>
  );
}

function ResourceDetail({
  selection,
  onClose,
  returnFocusRef,
}: {
  selection?: Selection;
  onClose: () => void;
  returnFocusRef: React.RefObject<HTMLElement | null>;
}) {
  const query = useQuery<{
    data: GatewayProvider | GatewayModel | GatewayMCPServer | GatewayRoute;
  }>({
    queryKey: ["connect-detail", selection?.kind, selection?.id, getScenario()],
    enabled: Boolean(selection),
    retry: false,
    queryFn: async ({ signal }) => {
      if (!selection) throw new Error("No resource selected");
      const options = { signal, path: { resourceId: selection.id } };
      if (selection.kind === "provider") return await requestOperation("getProvider", options);
      if (selection.kind === "model") return await requestOperation("getModel", options);
      if (selection.kind === "mcp") return await requestOperation("getGatewayMcpServer", options);
      return await requestOperation("getTrafficRoute", options);
    },
  });
  const item = query.data?.data;
  return (
    <DetailDrawer
      eyebrow="agentgateway resource"
      onClose={onClose}
      open={Boolean(selection)}
      returnFocusRef={returnFocusRef}
      title={item?.name ?? "Resource detail"}
    >
      {query.isLoading ? <div className="resource-note">Loading resource detail…</div> : null}
      {query.isError ? (
        <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
      ) : null}
      {item && selection ? <DefinitionList items={detailItems(selection, item)} /> : null}
    </DetailDrawer>
  );
}

function detailItems(
  selection: Selection,
  item: GatewayProvider | GatewayModel | GatewayMCPServer | GatewayRoute,
) {
  const common = [
    { label: "Source", value: <SourceBadge source={item.source} /> },
    { label: "Upstream ID", value: <code>{item.upstreamId ?? "Not provided"}</code> },
    { label: "Fetched", value: item.fetchedAt },
    { label: "Raw reference", value: <code>{item.rawRef.id}</code> },
  ];
  if (selection.kind === "provider") {
    const provider = item as GatewayProvider;
    return [
      { label: "Kind", value: provider.kind },
      { label: "Explicit model references", value: provider.modelCount },
      ...common,
    ];
  }
  if (selection.kind === "model") {
    const model = item as GatewayModel;
    return [
      { label: "Kind", value: model.kind },
      { label: "Provider", value: model.provider ?? "Not provided" },
      { label: "Routing", value: model.routing ?? "Not provided" },
      { label: "Targets", value: model.targets?.join(", ") || model.targetModel || "Not provided" },
      ...common,
    ];
  }
  if (selection.kind === "mcp") {
    const mcp = item as GatewayMCPServer;
    return [
      { label: "Transport", value: mcp.transport },
      { label: "Scope", value: <code>{mcp.scope}</code> },
      ...common,
    ];
  }
  const route = item as GatewayRoute;
  return [
    { label: "Listener", value: `${route.listener}:${route.port}` },
    { label: "Protocol", value: route.protocol },
    { label: "Hostnames", value: route.hostnames.join(", ") || "Not provided" },
    { label: "Path", value: route.path ?? "Not provided" },
    { label: "Backends", value: targetSummary(route) },
    ...common,
  ];
}

function Primary({
  icon: Icon,
  title,
  subtitle,
}: {
  icon: typeof Activity;
  title: string;
  subtitle: string;
}) {
  return (
    <div className="primary-cell">
      <Icon size={15} />
      <span>
        <strong>{title}</strong>
        <small>{subtitle}</small>
      </span>
    </div>
  );
}

function formatNullable(value: number | null) {
  return value === null ? "Not provided" : formatCount(value);
}

function targetSummary(route: GatewayRoute) {
  if (!route.targets.length) {
    return route.backendCount ? `${route.backendCount} configured; details unavailable` : "None";
  }
  return `${route.targets.join(", ")}${route.unavailableBackendCount > 0 ? ` · ${route.unavailableBackendCount} unavailable` : ""}`;
}
