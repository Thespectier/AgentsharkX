import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useBlocker } from "@tanstack/react-router";
import {
  Bot,
  Boxes,
  CheckCircle2,
  LoaderCircle,
  Pencil,
  RotateCcw,
  ScanSearch,
  ShieldQuestion,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { currentSection, PageFrame, WorkspaceTabs } from "../../components/workspace";
import {
  Button,
  Card,
  CardHeader,
  DataTable,
  DefinitionList,
  DetailDrawer,
  Dialog,
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
import type {
  LabelUpdate,
  TrustAgent,
  TrustAgentPageEnvelope,
  TrustResource,
  TrustResourcePageEnvelope,
  TrustResourceType,
  TrustScanJob,
} from "../../generated/api-client";
import { formatCount, formatTime } from "../../lib/format";
import { formatError, getScenario, mutateOperation, requestOperation } from "../../lib/api";

const tabs = [
  { id: "agents", label: "Agents" },
  { id: "resources", label: "Resources" },
  { id: "scans", label: "Scans" },
];

interface ScanRequest {
  agentId: string;
  resourceType: "skill" | "mcp";
  resourceIds: string[];
  useLlm?: boolean;
}

export function TrustPage() {
  const section = currentSection("trust", "agents");
  const queryClient = useQueryClient();
  const [activeJobId, setActiveJobId] = useState<string>();
  const [lastScanRequest, setLastScanRequest] = useState<ScanRequest>();
  const startScan = useMutation({
    mutationFn: async (request: ScanRequest) => {
      const path = { agentId: request.agentId };
      if (request.resourceType === "skill") {
        return mutateOperation(
          "detectSkills",
          { resourceIds: request.resourceIds, useLlm: request.useLlm ?? false },
          { path },
        );
      }
      return mutateOperation("detectMcps", { resourceIds: request.resourceIds }, { path });
    },
    onSuccess: (response, request) => {
      setLastScanRequest(request);
      setActiveJobId(response.data.id);
      void queryClient.invalidateQueries({ queryKey: ["trust-scans"] });
    },
  });
  const activeJob = useQuery({
    queryKey: ["trust-scan", activeJobId, getScenario()],
    enabled: Boolean(activeJobId),
    queryFn: ({ signal }) =>
      requestOperation("getTrustScan", { signal, path: { scanId: activeJobId! } }),
    refetchInterval: (query) => {
      const status = query.state.data?.data.status;
      return status === "queued" || status === "running" ? 500 : false;
    },
    retry: false,
  });
  const currentJob = activeJob.data?.data;
  const jobRunning = currentJob?.status === "queued" || currentJob?.status === "running";

  useBlocker({
    disabled: !jobRunning,
    enableBeforeUnload: jobRunning,
    shouldBlockFn: () =>
      jobRunning &&
      !window.confirm(
        "AgentGuard detection is still running. It will continue on the server if you leave this view.",
      ),
  });

  useEffect(() => {
    if (currentJob?.status !== "succeeded") return;
    void queryClient.invalidateQueries({ queryKey: ["trust-resources"] });
    void queryClient.invalidateQueries({ queryKey: ["trust-scans"] });
  }, [currentJob?.status, queryClient]);

  return (
    <PageFrame>
      <PageHeader
        description="Inspect only identities and resources reported explicitly by AgentGuard. Missing identity facts remain unknown."
        eyebrow="Trust / AgentGuard context"
        title="Know what every agent can reach"
      >
        <WorkspaceTabs area="trust" items={tabs} />
      </PageHeader>
      {activeJobId ? (
        <ScanActivity
          error={activeJob.isError ? formatError(activeJob.error) : undefined}
          job={currentJob}
          onRetry={() => lastScanRequest && startScan.mutate(lastScanRequest)}
          retrying={startScan.isPending}
        />
      ) : null}
      {section === "agents" ? <AgentsView /> : null}
      {section === "resources" ? (
        <ResourcesView
          onScan={(request) => {
            setLastScanRequest(request);
            startScan.mutate(request);
          }}
          scanPending={startScan.isPending}
        />
      ) : null}
      {section === "scans" ? <ScansView activeJob={currentJob} /> : null}
    </PageFrame>
  );
}

function AgentsView() {
  const pager = usePager();
  const [selected, setSelected] = useState<TrustAgent>();
  const returnFocusRef = useRef<HTMLElement | null>(null);
  const query = useQuery({
    queryKey: ["trust-agents", getScenario(), pager.search, pager.cursor],
    queryFn: ({ signal }) =>
      requestOperation("listAgents", {
        signal,
        query: { q: pager.search, cursor: pager.cursor, limit: 25 },
      }),
    retry: false,
  });
  if (query.isLoading) return <PageSkeleton label="Loading explicit AgentGuard identities" />;
  if (query.isError || !query.data)
    return (
      <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
    );

  const columns: Column<TrustAgent>[] = [
    {
      key: "agent",
      header: "Agent",
      render: (item) => <Primary icon={Bot} title={item.name} subtitle={item.upstreamId} />,
    },
    {
      key: "status",
      header: "Status",
      render: (item) => (
        <span className="status-cell">
          <StatusOrb status={item.status} /> {item.status}
        </span>
      ),
    },
    { key: "framework", header: "Framework", render: (item) => item.framework ?? "Unknown" },
    {
      key: "principal",
      header: "Principal",
      render: (item) => <code>{item.principal ?? "unknown"}</code>,
    },
    { key: "sessions", header: "Sessions", render: (item) => formatCount(item.sessions) },
    {
      key: "resources",
      header: "Resources",
      render: (item) => formatCount(item.tools + item.skills + item.mcps),
    },
    {
      key: "activity",
      header: "Last active",
      render: (item) => (item.lastActive ? `${formatTime(item.lastActive)} UTC` : "Unknown"),
    },
    { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
  ];
  return (
    <>
      <PartialBanner meta={query.data.meta} />
      <div className="trust-layout">
        <Card className="trust-distribution">
          <CardHeader
            description="No trust level is inferred when AgentGuard omits it."
            title="Identity boundary"
          />
          <div className="trust-note">
            <ShieldQuestion size={17} />
            <p>
              Every row originates from an explicit AgentGuard agent_id. This is contextual
              metadata, not remote attestation or cryptographic identity.
            </p>
          </div>
          <DefinitionList
            items={[
              { label: "Explicit agents", value: query.data.data.total },
              { label: "Inferred from gateway", value: "0" },
              {
                label: "Unknown trust levels",
                value: query.data.data.items.filter((item) => item.trustLevel === null).length,
              },
            ]}
          />
        </Card>
        <Card className="trust-table">
          <CardHeader
            description="Select an agent to inspect its explicit sessions and resources."
            title="Observed agents"
          />
          <ResourceControls pager={pager} page={query.data.data} fetching={query.isFetching} />
          {query.data.data.items.length ? (
            <DataTable
              columns={columns}
              data={query.data.data.items}
              label="Observed AgentGuard agents"
              onRowClick={(item, trigger) => {
                returnFocusRef.current = trigger;
                setSelected(item);
              }}
            />
          ) : (
            <EmptyState
              description="AgentGuard has not reported an explicit agent_id through sessions or resources. Gateway clients are never promoted into identities."
              title="No explicit agents reported"
            />
          )}
        </Card>
      </div>
      <AgentWorkspace
        agent={selected}
        onClose={() => setSelected(undefined)}
        returnFocusRef={returnFocusRef}
      />
    </>
  );
}

function AgentWorkspace({
  agent,
  onClose,
  returnFocusRef,
}: {
  agent?: TrustAgent;
  onClose: () => void;
  returnFocusRef: React.RefObject<HTMLElement | null>;
}) {
  const query = useQuery({
    queryKey: ["trust-agent", agent?.id, getScenario()],
    enabled: Boolean(agent),
    queryFn: ({ signal }) => requestOperation("getAgent", { signal, path: { agentId: agent!.id } }),
    retry: false,
  });
  const workspace = query.data?.data;
  return (
    <DetailDrawer
      eyebrow="AgentGuard identity workspace"
      onClose={onClose}
      open={Boolean(agent)}
      returnFocusRef={returnFocusRef}
      title={workspace?.agent.name ?? agent?.name ?? "Agent"}
    >
      {query.isLoading ? (
        <div className="resource-note">Loading explicit workspace facts…</div>
      ) : null}
      {query.isError ? (
        <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
      ) : null}
      {workspace ? (
        <div className="agent-workspace">
          <DefinitionList
            items={[
              { label: "Upstream ID", value: <code>{workspace.agent.upstreamId}</code> },
              { label: "Framework", value: workspace.agent.framework ?? "Unknown" },
              { label: "Principal", value: workspace.agent.principal ?? "Unknown" },
              { label: "Trust level", value: workspace.agent.trustLevel ?? "Unknown" },
              { label: "Source", value: <SourceBadge source={workspace.agent.source} /> },
              { label: "Raw reference", value: <code>{workspace.agent.rawRef.id}</code> },
            ]}
          />
          <Card>
            <CardHeader title={`Sessions (${workspace.sessions.length})`} />
            {workspace.sessions.length ? (
              <ul className="compact-list">
                {workspace.sessions.map((session) => (
                  <li key={session.id}>
                    <code>{session.upstreamId}</code>
                    <span>{session.userId ?? "Unknown principal"}</span>
                    <time>
                      {session.lastSeen ? `${formatTime(session.lastSeen)} UTC` : "Unknown"}
                    </time>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="resource-note">No explicit sessions.</p>
            )}
          </Card>
          <Card>
            <CardHeader title={`Resources (${workspace.resources.length})`} />
            {workspace.resources.length ? (
              <ul className="compact-list">
                {workspace.resources.map((resource) => (
                  <li key={resource.id}>
                    <strong>{resource.name}</strong>
                    <StatusBadge status={resource.type} />
                    <code>{resource.upstreamId}</code>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="resource-note">No explicit resources.</p>
            )}
          </Card>
        </div>
      ) : null}
    </DetailDrawer>
  );
}

function ResourcesView({
  onScan,
  scanPending,
}: {
  onScan: (request: ScanRequest) => void;
  scanPending: boolean;
}) {
  const pager = usePager();
  const queryClient = useQueryClient();
  const [resourceType, setResourceType] = useState<TrustResourceType | "">("");
  const [selectedTool, setSelectedTool] = useState<TrustResource>();
  const [pendingLabelId, setPendingLabelId] = useState<string>();
  const queryKey = [
    "trust-resources",
    getScenario(),
    pager.search,
    pager.cursor,
    resourceType,
  ] as const;
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) =>
      requestOperation("listTrustResources", {
        signal,
        query: { q: pager.search, cursor: pager.cursor, limit: 25, type: resourceType },
      }),
    retry: false,
  });
  const labels = useMutation({
    mutationFn: ({ resource, update }: { resource: TrustResource; update: LabelUpdate }) =>
      mutateOperation("updateToolLabels", update, {
        path: { agentId: resource.ownerAgentId, tool: resource.id },
      }),
    onMutate: async ({ resource, update }) => {
      setPendingLabelId(resource.id);
      await queryClient.cancelQueries({ queryKey });
      const previous = queryClient.getQueryData<TrustResourcePageEnvelope>(queryKey);
      if (previous) {
        queryClient.setQueryData<TrustResourcePageEnvelope>(queryKey, {
          ...previous,
          data: {
            ...previous.data,
            items: previous.data.items.map((item) =>
              item.id === resource.id
                ? {
                    ...item,
                    labels: {
                      boundary: update.boundary ?? item.labels?.boundary ?? "unknown",
                      sensitivity: update.sensitivity ?? item.labels?.sensitivity ?? "unknown",
                      integrity: update.integrity ?? item.labels?.integrity ?? "unknown",
                      tags: update.tags ?? item.labels?.tags ?? [],
                    },
                  }
                : item,
            ),
          },
        });
      }
      return { previous };
    },
    onError: (_error, _input, context) => {
      if (context?.previous) queryClient.setQueryData(queryKey, context.previous);
    },
    onSuccess: (response) => {
      queryClient.setQueryData<TrustResourcePageEnvelope>(queryKey, (current) =>
        current
          ? {
              ...current,
              data: {
                ...current.data,
                items: current.data.items.map((item) =>
                  item.id === response.data.id ? response.data : item,
                ),
              },
            }
          : current,
      );
      setSelectedTool(undefined);
    },
    onSettled: () => setPendingLabelId(undefined),
  });

  if (query.isLoading) return <PageSkeleton label="Loading AgentGuard resources" />;
  if (query.isError || !query.data)
    return (
      <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
    );

  const columns: Column<TrustResource>[] = [
    {
      key: "resource",
      header: "Resource",
      render: (item) => <Primary icon={Boxes} title={item.name} subtitle={item.upstreamId} />,
    },
    { key: "type", header: "Type", render: (item) => <StatusBadge status={item.type} /> },
    { key: "agent", header: "Agent", render: (item) => <code>{item.ownerAgentUpstreamId}</code> },
    {
      key: "labels",
      header: "Labels / detector",
      render: (item) =>
        item.labels ? (
          <span className="status-cell">
            <StatusBadge status={item.labels.sensitivity} /> {item.labels.boundary}
          </span>
        ) : item.detection ? (
          <span className="status-cell">
            <StatusBadge status={item.detection.label ?? "detected"} /> {item.detection.riskLevel}
          </span>
        ) : (
          "Not scanned"
        ),
    },
    {
      key: "fetched",
      header: "Fetched",
      render: (item) => `${formatTime(item.fetchedAt)} UTC`,
    },
    { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
    {
      key: "actions",
      header: "Action",
      render: (item) =>
        item.type === "tool" ? (
          <Button
            aria-label={`Edit labels for ${item.name}`}
            disabled={pendingLabelId === item.id}
            onClick={(event) => {
              event.stopPropagation();
              setSelectedTool(item);
            }}
            size="sm"
            variant="ghost"
          >
            <Pencil size={13} /> {pendingLabelId === item.id ? "Saving labels…" : "Edit labels"}
          </Button>
        ) : (
          <Button
            aria-label={`Scan ${item.name}`}
            disabled={scanPending}
            onClick={(event) => {
              event.stopPropagation();
              onScan({
                agentId: item.ownerAgentId,
                resourceType: item.type as "skill" | "mcp",
                resourceIds: [item.id],
              });
            }}
            size="sm"
            variant="ghost"
          >
            <ScanSearch size={13} /> Scan
          </Button>
        ),
    },
  ];
  return (
    <>
      <PartialBanner meta={query.data.meta} />
      <Card>
        <CardHeader
          description="Tools, Skills, and MCP resources share a view but retain their upstream type, owner, IDs, and raw references."
          title="Runtime resources"
        />
        <div className="resource-filter-row">
          <ResourceControls pager={pager} page={query.data.data} fetching={query.isFetching} />
          <label>
            <span className="sr-only">Resource type</span>
            <select
              aria-label="Resource type"
              value={resourceType}
              onChange={(event) => {
                setResourceType(event.target.value as TrustResourceType | "");
                pager.reset();
              }}
            >
              <option value="">All resource types</option>
              <option value="tool">Tools</option>
              <option value="skill">Skills</option>
              <option value="mcp">MCP</option>
            </select>
          </label>
        </div>
        {query.data.data.items.length ? (
          <DataTable
            columns={columns}
            data={query.data.data.items}
            label="AgentGuard runtime resources"
          />
        ) : (
          <EmptyState
            description="No explicit AgentGuard resources match this query."
            title="No resources reported"
          />
        )}
      </Card>
      <LabelDialog
        error={labels.isError ? formatError(labels.error) : undefined}
        onClose={() => !labels.isPending && setSelectedTool(undefined)}
        onSave={(update) => selectedTool && labels.mutate({ resource: selectedTool, update })}
        pending={labels.isPending}
        resource={selectedTool}
      />
    </>
  );
}

function LabelDialog({
  resource,
  pending,
  error,
  onClose,
  onSave,
}: {
  resource?: TrustResource;
  pending: boolean;
  error?: string;
  onClose: () => void;
  onSave: (update: LabelUpdate) => void;
}) {
  const [boundary, setBoundary] = useState("");
  const [sensitivity, setSensitivity] = useState("");
  const [integrity, setIntegrity] = useState("");
  const [tags, setTags] = useState("");
  useEffect(() => {
    setBoundary(resource?.labels?.boundary ?? "");
    setSensitivity(resource?.labels?.sensitivity ?? "");
    setIntegrity(resource?.labels?.integrity ?? "");
    setTags(resource?.labels?.tags.join(", ") ?? "");
  }, [resource]);
  return (
    <Dialog
      description="The table updates optimistically, then AgentGuard's response becomes the final value."
      onClose={onClose}
      open={Boolean(resource)}
      title={resource ? `Labels for ${resource.name}` : "Tool labels"}
    >
      <form
        className="dialog-form"
        onSubmit={(event) => {
          event.preventDefault();
          onSave({
            boundary,
            sensitivity,
            integrity,
            tags: tags
              .split(",")
              .map((tag) => tag.trim())
              .filter(Boolean),
          });
        }}
      >
        {[
          ["Boundary", boundary, setBoundary],
          ["Sensitivity", sensitivity, setSensitivity],
          ["Integrity", integrity, setIntegrity],
        ].map(([label, value, update]) => (
          <label className="field" key={label as string}>
            <span>{label as string}</span>
            <input
              disabled={pending}
              maxLength={64}
              onChange={(event) => (update as (value: string) => void)(event.target.value)}
              required
              value={value as string}
            />
          </label>
        ))}
        <label className="field">
          <span>Tags (comma separated)</span>
          <input
            disabled={pending}
            onChange={(event) => setTags(event.target.value)}
            value={tags}
          />
        </label>
        {error ? (
          <div className="form-error" role="alert">
            {error}
          </div>
        ) : null}
        <div className="dialog-actions">
          <Button disabled={pending} onClick={onClose} type="button" variant="ghost">
            Cancel
          </Button>
          <Button disabled={pending} type="submit" variant="primary">
            {pending ? (
              <>
                <LoaderCircle className="spin" size={14} /> Saving labels…
              </>
            ) : (
              "Save labels"
            )}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function ScansView({ activeJob }: { activeJob?: TrustScanJob }) {
  const query = useQuery({
    queryKey: ["trust-scans", getScenario()],
    queryFn: ({ signal }) => requestOperation("listTrustScans", { signal, query: { limit: 100 } }),
    retry: false,
  });
  if (query.isLoading) return <PageSkeleton label="Loading AgentGuard detection jobs" />;
  if (query.isError || !query.data)
    return (
      <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
    );
  const jobs = [activeJob, ...query.data.data.items]
    .filter((job): job is TrustScanJob => Boolean(job))
    .filter((job, index, items) => items.findIndex((item) => item.id === job.id) === index);
  return jobs.length ? (
    <div className="scan-grid">
      {jobs.map((job) => (
        <Card as="article" className="scan-card" key={job.id}>
          <span className="scan-card__icon">
            <ScanSearch size={19} />
          </span>
          <div>
            <div className="scan-card__top">
              <StatusBadge status={job.resourceType} />
              <StatusBadge status={job.status} />
            </div>
            <h2>{job.agentUpstreamId}</h2>
            <p>{scanSummary(job)}</p>
            {job.results.map((result) => (
              <p key={result.resourceUpstreamId ?? result.name}>
                <strong>{result.name ?? result.resourceUpstreamId}</strong>:{" "}
                {result.label || "detected"} · {result.riskLevel}
              </p>
            ))}
            <footer>
              <span>{job.id}</span>
              <time>{formatTime(job.updatedAt)} UTC</time>
            </footer>
          </div>
        </Card>
      ))}
    </div>
  ) : (
    <EmptyState
      description="Trigger a Skill or MCP scan from Resources. The BFF retains only a bounded in-memory job history."
      title="No scans available"
    />
  );
}

function ScanActivity({
  job,
  error,
  retrying,
  onRetry,
}: {
  job?: TrustScanJob;
  error?: string;
  retrying: boolean;
  onRetry: () => void;
}) {
  if (error)
    return <ErrorState description={error} onRetry={onRetry} title="Scan status unavailable" />;
  if (!job)
    return (
      <div className="scan-activity" role="status">
        <LoaderCircle className="spin" size={17} /> Creating scan job…
      </div>
    );
  if (job.status === "queued" || job.status === "running")
    return (
      <div className="scan-activity" role="status">
        <LoaderCircle className="spin" size={17} />
        <div>
          <strong>Detection {job.status}</strong>
          <span>Server-reported state · no synthetic percentage</span>
        </div>
      </div>
    );
  if (job.status === "failed")
    return (
      <div className="scan-activity scan-activity--error" role="alert">
        <RotateCcw size={17} />
        <div>
          <strong>Detection failed</strong>
          <span>{job.error?.message ?? "AgentGuard did not complete the scan."}</span>
        </div>
        <Button disabled={retrying || job.error?.retryable === false} onClick={onRetry} size="sm">
          {retrying ? "Retrying…" : "Retry scan"}
        </Button>
      </div>
    );
  return (
    <div className="scan-activity scan-activity--success" role="status">
      <CheckCircle2 size={17} />
      <div>
        <strong>Detection succeeded</strong>
        <span>{job.results.length} AgentGuard result(s) received</span>
      </div>
    </div>
  );
}

type Pager = ReturnType<typeof usePager>;

function ResourceControls({
  pager,
  page,
  fetching,
}: {
  pager: Pager;
  page: { nextCursor: string | null; total: number };
  fetching: boolean;
}) {
  return (
    <div className="resource-toolbar">
      <label>
        <span className="sr-only">Filter Trust resources</span>
        <input
          placeholder="Filter explicit Trust data"
          value={pager.search}
          onChange={(event) => pager.setSearch(event.target.value)}
        />
      </label>
      <span>{page.total} total</span>
      <Button
        disabled={!pager.canPrevious || fetching}
        onClick={pager.previous}
        size="sm"
        variant="ghost"
      >
        Previous
      </Button>
      <Button
        disabled={!page.nextCursor || fetching}
        onClick={() => page.nextCursor && pager.next(page.nextCursor)}
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
    reset() {
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

function Primary({
  icon: Icon,
  title,
  subtitle,
}: {
  icon: typeof Bot;
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

function scanSummary(job: TrustScanJob) {
  if (job.status === "queued" || job.status === "running")
    return `AgentGuard detection is ${job.status}.`;
  if (job.status === "failed") return job.error?.message ?? "Detection failed.";
  if (job.warnings.length) return job.warnings.join(" · ");
  return `${job.results.length} detector result(s) returned.`;
}
