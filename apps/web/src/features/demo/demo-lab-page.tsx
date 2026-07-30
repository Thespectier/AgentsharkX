import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  CheckCircle2,
  CircleAlert,
  Clock3,
  ExternalLink,
  FlaskConical,
  Gauge,
  GitBranch,
  History,
  Link2,
  LoaderCircle,
  Network,
  Play,
  RefreshCw,
  ShieldCheck,
  Square,
  type LucideIcon,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import { TraceSpanDrawer } from "../../components/trace-span-drawer";
import {
  Button,
  Card,
  CardHeader,
  DataTable,
  DefinitionList,
  Dialog,
  ErrorState,
  InlineLoading,
  PageHeader,
  PageSkeleton,
  SeverityBadge,
  StatusBadge,
  StatusOrb,
  cn,
  type Column,
} from "../../components/ui";
import { PageFrame, useDocumentTitle } from "../../components/workspace";
import type {
  DemoCorrelationEvidence,
  DemoRun,
  DemoRunEvent,
  DemoScenario,
  DemoScenarioDefinition,
  DemoStatus,
  DemoStatusComponent,
  ProtectMutationReceipt,
  TraceDetail,
} from "../../generated/api-client";
import {
  ApiError,
  formatError,
  getScenario,
  mutateOperation,
  requestOperation,
} from "../../lib/api";
import { formatCount, formatDateTimeWithZone } from "../../lib/format";
import { useI18n } from "../../lib/i18n";
import { isDemoRunTerminal, useDemoRunEvents } from "../../lib/use-demo-run-events";
import {
  ApprovalDecisionDialog,
  approvalSeverity,
  ProtectMutationReceiptNotice,
} from "../protect/approval-decision";
import { TraceVisualization } from "../audit/trace-visualization";
import {
  clampDemoDelay,
  createDemoRequestId,
  demoApprovalRecord,
  demoMetricComparisons,
  formatDemoRunDuration,
} from "./demo-model";

const demoHistoryLimit = 10;

export function DemoLabPage() {
  useDocumentTitle("Demo Lab");
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const mockScenario = getScenario();
  const [scenario, setScenario] = useState<DemoScenario>("approval");
  const [delayMs, setDelayMs] = useState(700);
  const [focusedRunId, setFocusedRunId] = useState<string>();
  const [selectedSpanId, setSelectedSpanId] = useState<string>();
  const [cancelOpen, setCancelOpen] = useState(false);
  const [cancelNote, setCancelNote] = useState("");
  const [cancelConfirmed, setCancelConfirmed] = useState(false);
  const [approvalOpen, setApprovalOpen] = useState(false);
  const [receipt, setReceipt] = useState<ProtectMutationReceipt>();
  const spanTriggerRef = useRef<HTMLElement | null>(null);
  const requestIdRef = useRef<string | undefined>(undefined);

  const statusQuery = useQuery({
    queryKey: ["demo-status", mockScenario],
    queryFn: ({ signal }) => requestOperation("getDemoStatus", signal),
    retry: false,
  });
  const demoEnabled = statusQuery.data?.data.enabled === true;
  const scenariosQuery = useQuery({
    queryKey: ["demo-scenarios", mockScenario],
    queryFn: ({ signal }) => requestOperation("listDemoScenarios", signal),
    enabled: demoEnabled,
    retry: false,
  });
  const historyQuery = useQuery({
    queryKey: ["demo-runs", mockScenario],
    queryFn: ({ signal }) =>
      requestOperation("listDemoRuns", { signal, query: { limit: demoHistoryLimit } }),
    enabled: demoEnabled,
    retry: false,
  });
  const activeRunId = statusQuery.data?.data.activeRunId ?? undefined;
  const selectedRunId = activeRunId ?? focusedRunId ?? historyQuery.data?.data.items[0]?.runId;
  const runQuery = useQuery({
    queryKey: ["demo-run", selectedRunId, mockScenario],
    queryFn: ({ signal }) =>
      requestOperation("getDemoRun", { signal, path: { runId: selectedRunId! } }),
    enabled: demoEnabled && Boolean(selectedRunId),
    retry: false,
  });
  const run = runQuery.data?.data;
  const stream = useDemoRunEvents({
    runId: run?.runId,
    enabled: demoEnabled && Boolean(run),
    terminal: isDemoRunTerminal(run?.status),
  });
  const traceQuery = useQuery({
    queryKey: ["audit-trace", run?.traceId, mockScenario],
    queryFn: ({ signal }) =>
      requestOperation("getAuditTrace", { signal, path: { traceId: run!.traceId! } }),
    enabled: demoEnabled && Boolean(run?.traceId),
    retry: false,
  });

  useEffect(() => setSelectedSpanId(undefined), [run?.traceId]);
  useEffect(() => {
    setApprovalOpen(false);
    setReceipt(undefined);
    setCancelOpen(false);
  }, [run?.runId]);

  const start = useMutation({
    mutationFn: () => {
      requestIdRef.current ??= createDemoRequestId();
      return mutateOperation(
        "createDemoRun",
        { scenario, delayMs },
        { requestId: requestIdRef.current },
      );
    },
    onSuccess: (response) => {
      requestIdRef.current = undefined;
      setFocusedRunId(response.data.runId);
      queryClient.setQueryData(["demo-run", response.data.runId, mockScenario], response);
      void queryClient.invalidateQueries({ queryKey: ["demo-status"] });
      void queryClient.invalidateQueries({ queryKey: ["demo-runs"] });
    },
    onError: () => {
      void queryClient.invalidateQueries({ queryKey: ["demo-status"] });
    },
  });
  const cancel = useMutation({
    mutationFn: (target: DemoRun) =>
      mutateOperation(
        "cancelDemoRun",
        { confirm: cancelConfirmed, note: cancelNote },
        { path: { runId: target.runId } },
      ),
    onSuccess: (response) => {
      queryClient.setQueryData(["demo-run", response.data.runId, mockScenario], response);
      setCancelOpen(false);
      setCancelNote("");
      setCancelConfirmed(false);
      void queryClient.invalidateQueries({ queryKey: ["demo-status"] });
      void queryClient.invalidateQueries({ queryKey: ["demo-runs"] });
    },
  });

  const resetStartIntent = () => {
    requestIdRef.current = undefined;
    start.reset();
  };
  const updateScenario = (next: DemoScenario) => {
    resetStartIntent();
    setScenario(next);
  };
  const updateDelay = (next: number) => {
    resetStartIntent();
    setDelayMs(clampDemoDelay(next));
  };
  const handleApprovalReceipt = (nextReceipt: ProtectMutationReceipt) => {
    setReceipt(nextReceipt);
    void queryClient.invalidateQueries({ queryKey: ["demo-run", run?.runId] });
    void queryClient.invalidateQueries({ queryKey: ["demo-status"] });
    void queryClient.invalidateQueries({ queryKey: ["demo-runs"] });
  };

  if (statusQuery.isLoading) {
    return (
      <PageFrame>
        <DemoHeader />
        <PageSkeleton label="Loading Demo Lab readiness" />
      </PageFrame>
    );
  }
  if (statusQuery.isError || !statusQuery.data) {
    return (
      <PageFrame>
        <DemoHeader />
        <ErrorState
          description={formatError(statusQuery.error)}
          onRetry={() => void statusQuery.refetch()}
          title="Demo Lab status unavailable"
        />
      </PageFrame>
    );
  }

  const status = statusQuery.data.data;
  const trace = traceQuery.data?.data;
  const pendingApproval = demoApprovalRecord(run?.approval);
  const selectedSpan = trace?.spans.find((span) => span.spanId === selectedSpanId);

  return (
    <PageFrame>
      <DemoHeader onRefresh={() => void statusQuery.refetch()} />
      <DemoReadiness status={status} />
      {!status.enabled ? (
        <DemoDisabled />
      ) : (
        <>
          <div className="demo-lab-layout">
            <DemoControls
              activeRunId={status.activeRunId}
              delayMs={delayMs}
              onDelayChange={updateDelay}
              onScenarioChange={updateScenario}
              onStart={() => start.mutate()}
              ready={status.ready}
              scenario={scenario}
              scenarios={scenariosQuery.data?.data}
              scenariosError={scenariosQuery.error}
              scenariosLoading={scenariosQuery.isLoading}
              startError={start.error}
              startPending={start.isPending}
            />
            <ActiveRunPanel
              events={stream.events}
              loading={runQuery.isLoading}
              onCancel={() => setCancelOpen(true)}
              run={run}
              runError={runQuery.error}
              streamStatus={stream.status}
              trace={trace}
            />
            <ApprovalPanel onReview={() => setApprovalOpen(true)} receipt={receipt} run={run} />
            <DemoTracePanel
              error={traceQuery.error}
              loading={traceQuery.isLoading}
              onSelect={(spanId, trigger) => {
                if (trigger) spanTriggerRef.current = trigger;
                setSelectedSpanId(spanId);
              }}
              run={run}
              selectedSpanId={selectedSpanId}
              trace={trace}
            />
          </div>
          <CapabilityEvidence run={run} />
          <DemoHistory
            error={historyQuery.error}
            loading={historyQuery.isLoading}
            onSelect={setFocusedRunId}
            runs={historyQuery.data?.data.items ?? []}
            selectedRunId={selectedRunId}
            selectedTrace={trace}
          />
        </>
      )}
      {run?.traceId ? (
        <TraceSpanDrawer
          onClose={() => setSelectedSpanId(undefined)}
          returnFocusRef={spanTriggerRef}
          span={selectedSpan}
          spanId={selectedSpanId}
          traceId={run.traceId}
        />
      ) : null}
      {approvalOpen && pendingApproval ? (
        <ApprovalDecisionDialog
          approval={pendingApproval}
          onClose={() => setApprovalOpen(false)}
          onReceipt={handleApprovalReceipt}
        />
      ) : null}
      <Dialog
        description="Cancellation is cooperative and requires an explicit operator note."
        onClose={() => !cancel.isPending && setCancelOpen(false)}
        open={cancelOpen && Boolean(run)}
        title="Cancel Demo Run"
      >
        <div className="dialog-form">
          <label className="field">
            <span>{t("Operator note")}</span>
            <textarea
              aria-label={t("Operator note")}
              onChange={(event) => setCancelNote(event.target.value)}
              rows={3}
              value={cancelNote}
            />
          </label>
          <label className="confirm-field">
            <input
              checked={cancelConfirmed}
              onChange={(event) => setCancelConfirmed(event.target.checked)}
              type="checkbox"
            />
            {t("I confirm this active simulated Run should be cancelled.")}
          </label>
          {cancel.isError ? <DemoMutationError error={cancel.error} /> : null}
          <footer>
            <Button
              disabled={cancel.isPending}
              onClick={() => setCancelOpen(false)}
              variant="ghost"
            >
              Cancel
            </Button>
            <Button
              disabled={!run || !cancelNote.trim() || !cancelConfirmed || cancel.isPending}
              onClick={() => run && cancel.mutate(run)}
              variant="danger"
            >
              {cancel.isPending ? (
                <LoaderCircle className="spin" size={14} />
              ) : (
                <Square size={13} />
              )}
              {cancel.isError ? "Retry cancellation" : "Cancel Run"}
            </Button>
          </footer>
        </div>
      </Dialog>
    </PageFrame>
  );
}

function DemoHeader({ onRefresh }: { onRefresh?: () => void }) {
  const { t } = useI18n();
  return (
    <PageHeader
      actions={
        onRefresh ? (
          <Button onClick={onRefresh} variant="secondary">
            <RefreshCw aria-hidden="true" size={14} /> Refresh
          </Button>
        ) : undefined
      }
      description="Run a fixed simulated incident workflow through Agentshark connection, runtime protection, SDK, and Trace management paths."
      eyebrow="Tools / Demo Lab"
      title="Demo Lab"
    >
      <div className="demo-header-strip">
        <span className="demo-fixture-badge">
          <FlaskConical aria-hidden="true" size={14} /> {t("DETERMINISTIC FIXTURE")}
        </span>
        <span>{t("SIMULATED - no external side effects")}</span>
      </div>
    </PageHeader>
  );
}

function DemoReadiness({ status }: { status: DemoStatus }) {
  const { t } = useI18n();
  return (
    <Card className="demo-readiness">
      <CardHeader
        action={
          <StatusBadge
            status={!status.enabled ? "disabled" : status.ready ? "ready" : "not ready"}
          />
        }
        description="Required components are reported independently; one failure never hides the other checks."
        title="Readiness"
      />
      <div className="demo-readiness__grid">
        {status.components.map((component) => (
          <ReadinessComponent component={component} enabled={status.enabled} key={component.id} />
        ))}
      </div>
      {!status.ready && status.enabled ? (
        <div className="demo-readiness__warning" role="status">
          <CircleAlert aria-hidden="true" size={16} />
          {t("Start is unavailable until every required component is healthy.")}
        </div>
      ) : null}
    </Card>
  );
}

function ReadinessComponent({
  component,
  enabled,
}: {
  component: DemoStatusComponent;
  enabled: boolean;
}) {
  const { t } = useI18n();
  const displayStatus = enabled ? component.status : "disabled";
  return (
    <article className="demo-readiness__item">
      <div>
        <StatusOrb
          label={`${component.label} ${displayStatus}`}
          status={enabled ? component.status : "unknown"}
        />
        <strong>{t(component.label)}</strong>
        {component.required ? <span>{t("Required")}</span> : null}
      </div>
      <StatusBadge status={displayStatus} />
      <time dateTime={component.checkedAt}>{formatDateTimeWithZone(component.checkedAt)}</time>
      {component.message ? <p>{t(component.message)}</p> : null}
      {displayStatus !== "healthy" && component.remediation ? (
        <small>{t(component.remediation)}</small>
      ) : null}
    </article>
  );
}

function DemoDisabled() {
  const { t } = useI18n();
  return (
    <section className="demo-disabled" role="status">
      <FlaskConical aria-hidden="true" size={26} />
      <div>
        <h2>{t("Demo Lab is disabled")}</h2>
        <p>
          {t(
            "Enable the explicit Demo deployment configuration before scenarios, Runner controls, or fixtures become available.",
          )}
        </p>
      </div>
    </section>
  );
}

function DemoControls({
  activeRunId,
  delayMs,
  onDelayChange,
  onScenarioChange,
  onStart,
  ready,
  scenario,
  scenarios,
  scenariosError,
  scenariosLoading,
  startError,
  startPending,
}: {
  activeRunId: string | null;
  delayMs: number;
  onDelayChange: (value: number) => void;
  onScenarioChange: (scenario: DemoScenario) => void;
  onStart: () => void;
  ready: boolean;
  scenario: DemoScenario;
  scenarios?: DemoScenarioDefinition[];
  scenariosError: Error | null;
  scenariosLoading: boolean;
  startError: Error | null;
  startPending: boolean;
}) {
  const { t } = useI18n();
  const disabled = !ready || Boolean(activeRunId) || startPending || !scenarios?.length;
  return (
    <Card className="demo-controls">
      <CardHeader
        action={<StatusBadge status={activeRunId ? "busy" : ready ? "ready" : "not ready"} />}
        description="Choose one immutable workflow. Prompts, targets, URLs, and commands cannot be supplied."
        title="Scenario controls"
      />
      <div className="demo-controls__body">
        {scenariosLoading ? <InlineLoading label="Loading fixed scenarios" /> : null}
        {scenariosError ? <DemoMutationError error={scenariosError} /> : null}
        <div className="demo-scenarios" role="radiogroup" aria-label={t("Demo scenario")}>
          {scenarios?.map((definition) => (
            <label
              className={cn(
                "demo-scenario-option",
                scenario === definition.id && "demo-scenario-option--selected",
              )}
              key={definition.id}
            >
              <input
                checked={scenario === definition.id}
                name="demo-scenario"
                onChange={() => onScenarioChange(definition.id)}
                type="radio"
              />
              <span>
                <strong>{t(definition.label)}</strong>
                <small>{t(definition.description)}</small>
                <em>
                  LLM {definition.expectedMetrics.llmCalls} / MCP{" "}
                  {definition.expectedMetrics.mcpCalls} / A2A {definition.expectedMetrics.a2aCalls}
                </em>
              </span>
            </label>
          ))}
        </div>
        <div className="demo-delay-control">
          <label htmlFor="demo-delay">{t("Step delay")}</label>
          <input
            id="demo-delay"
            max="2000"
            min="0"
            onChange={(event) => onDelayChange(Number(event.target.value))}
            step="50"
            type="range"
            value={delayMs}
          />
          <label className="demo-delay-value">
            <span className="sr-only">{t("Step delay in milliseconds")}</span>
            <input
              aria-label={t("Step delay in milliseconds")}
              max="2000"
              min="0"
              onChange={(event) => onDelayChange(Number(event.target.value))}
              type="number"
              value={delayMs}
            />
            ms
          </label>
        </div>
        {activeRunId ? (
          <p className="demo-control-note">
            <Clock3 aria-hidden="true" size={14} />{" "}
            {t("One Run is active; start remains disabled.")}
          </p>
        ) : null}
        {startError ? <DemoMutationError error={startError} /> : null}
        <Button disabled={disabled} onClick={onStart} variant="primary">
          {startPending ? <LoaderCircle className="spin" size={14} /> : <Play size={14} />}
          {startError ? "Retry start" : "Start Run"}
        </Button>
      </div>
    </Card>
  );
}

function ActiveRunPanel({
  events,
  loading,
  onCancel,
  run,
  runError,
  streamStatus,
  trace,
}: {
  events: DemoRunEvent[];
  loading: boolean;
  onCancel: () => void;
  run?: DemoRun;
  runError: Error | null;
  streamStatus: string;
  trace?: TraceDetail;
}) {
  const { t } = useI18n();
  if (loading) {
    return (
      <Card className="demo-active-run">
        <CardHeader title="Active Run" />
        <div className="demo-section-loading">
          <InlineLoading label="Loading Demo Run" />
        </div>
      </Card>
    );
  }
  if (runError) {
    return (
      <Card className="demo-active-run">
        <CardHeader title="Active Run" />
        <ErrorState description={formatError(runError)} title="Demo Run unavailable" />
      </Card>
    );
  }
  if (!run) {
    return (
      <Card className="demo-active-run">
        <CardHeader title="Active Run" />
        <div className="demo-empty-section">
          <Gauge aria-hidden="true" size={24} />
          <p>{t("Start a scenario or select a recent Run to inspect its evidence.")}</p>
        </div>
      </Card>
    );
  }
  const metrics = demoMetricComparisons(run, trace?.summary);
  const progress = run.totalSteps ? Math.min(100, (run.completedSteps / run.totalSteps) * 100) : 0;
  return (
    <Card className="demo-active-run">
      <CardHeader
        action={
          <div className="demo-run-statuses">
            <StatusBadge status={run.status.replaceAll("_", " ")} />
            <StatusBadge status={run.outcome} />
          </div>
        }
        description="Persisted control state is refreshed by the Run stream and isolated polling fallback."
        title="Active Run"
      />
      <div className="demo-active-run__body">
        <div className="demo-run-toolbar">
          <code>{run.runId}</code>
          <span className={`demo-stream-state demo-stream-state--${streamStatus}`}>
            <span /> {t(streamStatus === "polling" ? "Polling fallback" : streamStatus)}
          </span>
          {!isDemoRunTerminal(run.status) ? (
            <Button onClick={onCancel} size="sm" variant="danger">
              <Square aria-hidden="true" size={12} /> Cancel Run
            </Button>
          ) : null}
        </div>
        <DefinitionList
          items={[
            { label: "Scenario", value: run.scenario },
            { label: "Task ID", value: <code>{run.taskId}</code> },
            { label: "Session ID", value: <code>{run.sessionId}</code> },
            { label: "Trace ID", value: run.traceId ? <code>{run.traceId}</code> : "Pending" },
            { label: "Current step", value: run.currentStep || "Pending" },
            { label: "Trace completeness", value: trace?.summary.completeness ?? "Pending" },
          ]}
        />
        <div className="demo-progress" aria-label={t("Run progress")}>
          <div>
            <span>{t("Progress")}</span>
            <strong>
              {run.completedSteps} / {run.totalSteps}
            </strong>
          </div>
          <span>
            <i style={{ width: `${progress}%` }} />
          </span>
        </div>
        <div className="demo-metric-grid">
          {metrics.map((metric) => (
            <article
              className={cn(
                "demo-metric",
                metric.matches === true && "demo-metric--match",
                metric.matches === false && "demo-metric--mismatch",
                metric.matches === null && "demo-metric--pending",
              )}
              key={metric.key}
            >
              <span>{t(metric.label)}</span>
              <strong>{t(metric.display)}</strong>
              <small>
                {metric.expected === null
                  ? t(metric.matches === null ? "Awaiting Trace evidence" : "Observed Trace value")
                  : t("Expected {count}", { count: metric.expected })}
              </small>
            </article>
          ))}
        </div>
        <RunTimeline events={events} run={run} />
        <DemoRunResult run={run} />
      </div>
    </Card>
  );
}

function DemoRunResult({ run }: { run: DemoRun }) {
  const { t } = useI18n();
  if (!isDemoRunTerminal(run.status)) return null;

  const failed = ["failed", "interrupted", "expired"].includes(run.status);
  const cancelled = run.status === "cancelled";
  const Icon = failed ? CircleAlert : cancelled ? Square : CheckCircle2;
  const title =
    run.status === "failed"
      ? "Run failed"
      : run.status === "interrupted"
        ? "Run interrupted"
        : run.status === "expired"
          ? "Run expired"
          : run.status === "cancelled"
            ? "Run cancelled"
            : "Result";
  const detail = run.errorSummary || run.statusReasonCode || run.outcome;
  return (
    <div
      className={cn(
        "demo-result",
        failed && "demo-result--error",
        cancelled && "demo-result--neutral",
      )}
      role="status"
    >
      <Icon aria-hidden="true" size={16} />
      <span>
        <strong>{run.errorCode || t(title)}</strong>
        {t(detail.replaceAll("_", " "))}
      </span>
    </div>
  );
}

function RunTimeline({ events, run }: { events: DemoRunEvent[]; run: DemoRun }) {
  const { t } = useI18n();
  const entries = events.length
    ? events.map((event) => ({
        key: `${event.runVersion}:${event.type}`,
        label: event.stepId || event.status || event.type.replace("run.", ""),
        time: event.occurredAt,
      }))
    : [
        { key: "requested", label: "Requested", time: run.requestedAt },
        ...(run.startedAt ? [{ key: "started", label: "Started", time: run.startedAt }] : []),
        ...(run.approval
          ? [{ key: "approval", label: "Waiting approval", time: run.approval.createdAt }]
          : []),
        ...(run.completedAt
          ? [{ key: "completed", label: run.status, time: run.completedAt }]
          : []),
      ];
  return (
    <div className="demo-timeline" aria-label={t("Status timeline")}>
      {entries.slice(-6).map((entry) => (
        <div key={entry.key}>
          <span />
          <strong>{t(entry.label.replaceAll("_", " "))}</strong>
          <time dateTime={entry.time}>{formatDateTimeWithZone(entry.time)}</time>
        </div>
      ))}
    </div>
  );
}

function ApprovalPanel({
  onReview,
  receipt,
  run,
}: {
  onReview: () => void;
  receipt?: ProtectMutationReceipt;
  run?: DemoRun;
}) {
  const { t } = useI18n();
  const approval = run?.approval;
  const pending = demoApprovalRecord(approval);
  const evidence = run?.correlations.approval;
  return (
    <Card className="demo-approval">
      <CardHeader
        action={evidence ? <StatusBadge status={evidence.status} /> : undefined}
        description="Approval remains a runtime protection record and is correlated only by an identical reported session ID."
        title="Security / Approval"
      />
      <div className="demo-approval__body">
        {receipt ? <ProtectMutationReceiptNotice receipt={receipt} /> : null}
        {!run ? (
          <p className="demo-placeholder">{t("No Run is selected.")}</p>
        ) : approval ? (
          <>
            <div className="demo-approval__heading">
              <SeverityBadge severity={approvalSeverity(approval.riskScore)} />
              <strong>{approval.tool || approval.eventType}</strong>
              <StatusBadge status={approval.status} />
            </div>
            <p>{approval.reason || t("No upstream reason was provided.")}</p>
            <DefinitionList
              items={[
                { label: "Ticket ID", value: <code>{approval.ticketId}</code> },
                { label: "Session ID", value: <code>{approval.sessionId}</code> },
                { label: "Phase", value: approval.phase },
                { label: "Matched rules", value: approval.matchedRules.join(", ") || "None" },
              ]}
            />
            <div className="demo-correlation-label">
              <Link2 aria-hidden="true" size={14} />
              <span>
                <strong>
                  {evidence?.status === "verified"
                    ? t("Linked by exact session_id")
                    : t("Approval correlation pending")}
                </strong>
                {approval.correlationBasis}
              </span>
            </div>
            {pending && run.status === "waiting_approval" ? (
              <Button onClick={onReview} variant="primary">
                <ShieldCheck aria-hidden="true" size={14} /> Review decision
              </Button>
            ) : null}
          </>
        ) : (
          <p className="demo-placeholder">
            {t(
              run.scenario === "approval"
                ? "Waiting for the exact-session approval ticket."
                : "This scenario does not expect a human approval.",
            )}
          </p>
        )}
      </div>
    </Card>
  );
}

function DemoTracePanel({
  error,
  loading,
  onSelect,
  run,
  selectedSpanId,
  trace,
}: {
  error: Error | null;
  loading: boolean;
  onSelect: (spanId: string, trigger?: HTMLElement) => void;
  run?: DemoRun;
  selectedSpanId?: string;
  trace?: TraceDetail;
}) {
  const { t } = useI18n();
  return (
    <Card className="demo-trace">
      <CardHeader
        action={
          run?.links.trace ? (
            <a className="button button--secondary button--sm" href={run.links.trace}>
              {t("Open full Trace")} <ExternalLink aria-hidden="true" size={13} />
            </a>
          ) : undefined
        }
        description="Switch between the explicit relationship flow and the absolute-time execution timeline."
        title="Live Trace"
      />
      <div
        className={cn(
          "demo-trace__body",
          !loading && !error && trace && "demo-trace__body--visualization",
        )}
      >
        {loading ? <InlineLoading label="Loading linked Trace" /> : null}
        {error ? (
          <ErrorState description={formatError(error)} title="Linked Trace unavailable" />
        ) : null}
        {!loading && !error && trace ? (
          <>
            <div className="demo-trace__summary">
              <StatusBadge status={trace.summary.status} />
              <StatusBadge status={trace.summary.completeness} />
              <span>
                {formatCount(trace.totalSpans)} {t("spans")}
              </span>
              <code>{trace.summary.traceId}</code>
            </div>
            <TraceVisualization
              isLive={trace.summary.status === "running"}
              onSelectSpan={onSelect}
              selectedSpanId={selectedSpanId}
              trace={trace}
            />
          </>
        ) : null}
        {!loading && !error && !trace ? (
          <div className="demo-placeholder demo-placeholder--trace">
            <GitBranch aria-hidden="true" size={22} />
            <p>
              {t(
                run
                  ? run.correlations.trace.status === "unavailable"
                    ? "Trace evidence is unavailable for this Run."
                    : "Waiting for an explicitly linked Trace ID."
                  : "Select or start a Run to load its real Trace.",
              )}
            </p>
          </div>
        ) : null}
      </div>
    </Card>
  );
}

function CapabilityEvidence({ run }: { run?: DemoRun }) {
  const { t } = useI18n();
  const evidence = useMemo(() => capabilityRows(run), [run]);
  return (
    <Card className="demo-evidence">
      <CardHeader
        description="Only explicitly observed source evidence is marked verified; unavailable associations remain visible."
        title="Capability Evidence"
      />
      <div className="demo-evidence__grid">
        {evidence.map((item) => {
          const Icon = item.icon;
          return (
            <article className="demo-evidence__item" key={item.area}>
              <span className="demo-evidence__icon">
                <Icon aria-hidden="true" size={18} />
              </span>
              <div>
                <header>
                  <strong>{item.area}</strong>
                  <StatusBadge status={item.status} />
                </header>
                <p>{t(item.detail, item.variables)}</p>
                <small>{t(item.basis)}</small>
                {item.href ? (
                  <a href={item.href}>
                    {t("Open evidence")} <ExternalLink size={12} />
                  </a>
                ) : null}
              </div>
            </article>
          );
        })}
      </div>
    </Card>
  );
}

function capabilityRows(run?: DemoRun): Array<{
  area: string;
  icon: LucideIcon;
  status: DemoCorrelationEvidence["status"];
  detail: string;
  variables: Record<string, string | number>;
  basis: string;
  href?: string;
}> {
  const pending: DemoCorrelationEvidence = { status: "pending", basis: "No Run selected" };
  const trace = run?.correlations.trace ?? pending;
  const approval = run?.correlations.approval ?? pending;
  const gateway = run?.correlations.gatewayLogs ?? pending;
  const trustStatus =
    trace.status === "verified" && run?.observedMetrics ? "verified" : trace.status;
  return [
    {
      area: "Connect",
      icon: Network,
      status: gateway.status,
      detail:
        gateway.status === "verified"
          ? "All {count} expected LLM calls have exact connection log evidence."
          : "Connection request-log evidence is not yet verified.",
      variables: { count: run?.expectedMetrics.llmCalls ?? "Pending" },
      basis: gateway.basis,
    },
    {
      area: "Trust",
      icon: Activity,
      status: trustStatus,
      detail: run?.observedMetrics
        ? "{mcp} MCP, {local} local tool, and {a2a} A2A calls observed."
        : "Agent, MCP, tool, and peer evidence is pending Trace observation.",
      variables: {
        mcp: run?.observedMetrics?.mcpCalls ?? "Pending",
        local: run?.observedMetrics?.localToolCalls ?? "Pending",
        a2a: run?.observedMetrics?.a2aCalls ?? "Pending",
      },
      basis: trace.basis,
      href: run?.links.trace,
    },
    {
      area: "Protect",
      icon: ShieldCheck,
      status: approval.status,
      detail:
        approval.status === "verified"
          ? "Approval ticket {ticket} is session-linked."
          : "No exact-session approval evidence is verified.",
      variables: { ticket: run?.approval?.ticketId ?? "Pending" },
      basis: approval.basis,
      href: run?.links.approval,
    },
    {
      area: "Audit",
      icon: GitBranch,
      status: trace.status,
      detail:
        trace.status === "verified"
          ? "Trace {traceId} is linked by its explicit task and session identity."
          : "Trace and error-span evidence is not yet verified.",
      variables: { traceId: run?.traceId ?? "Pending" },
      basis: trace.basis,
      href: run?.links.audit,
    },
  ];
}

function DemoHistory({
  error,
  loading,
  onSelect,
  runs,
  selectedRunId,
  selectedTrace,
}: {
  error: Error | null;
  loading: boolean;
  onSelect: (runId: string) => void;
  runs: DemoRun[];
  selectedRunId?: string;
  selectedTrace?: TraceDetail;
}) {
  const { t } = useI18n();
  const rows = runs.map((run) => ({ ...run, id: run.runId }));
  const columns: Column<DemoRun & { id: string }>[] = [
    {
      key: "run",
      header: "Run ID",
      render: (run) => (
        <div className="primary-cell">
          <History aria-hidden="true" size={15} />
          <span>
            <strong>{shortId(run.runId)}</strong>
            <small>{run.taskId}</small>
          </span>
        </div>
      ),
    },
    { key: "scenario", header: "Scenario", render: (run) => t(run.scenario) },
    { key: "status", header: "Status", render: (run) => <StatusBadge status={run.status} /> },
    { key: "outcome", header: "Outcome", render: (run) => <StatusBadge status={run.outcome} /> },
    {
      key: "completeness",
      header: "Trace completeness",
      render: (run) =>
        run.traceId && selectedTrace?.summary.traceId === run.traceId ? (
          <StatusBadge status={selectedTrace.summary.completeness} />
        ) : (
          t(run.traceId ? "Not loaded" : "Unavailable")
        ),
    },
    { key: "duration", header: "Duration", render: formatDemoRunDuration },
    {
      key: "started",
      header: "Started At",
      render: (run) => (
        <time dateTime={run.startedAt ?? run.requestedAt}>
          {formatDateTimeWithZone(run.startedAt ?? run.requestedAt)}
        </time>
      ),
    },
    {
      key: "trace",
      header: "Open Trace",
      render: (run) =>
        run.links.trace ? (
          <a
            className="demo-history-link"
            href={run.links.trace}
            onClick={(event) => event.stopPropagation()}
          >
            {t("Open")} <ExternalLink aria-hidden="true" size={12} />
          </a>
        ) : (
          t("Pending")
        ),
    },
  ];
  return (
    <Card className="demo-history">
      <CardHeader
        action={
          <span className="fetched-at">
            {formatCount(runs.length)} {t("recent Runs")}
          </span>
        }
        description="Persisted control records remain available across page and BFF restarts."
        title="Recent Demo Runs"
      />
      {loading ? (
        <div className="demo-section-loading">
          <InlineLoading label="Loading recent Demo Runs" />
        </div>
      ) : null}
      {error ? (
        <ErrorState description={formatError(error)} title="Demo Run history unavailable" />
      ) : null}
      {!loading && !error && rows.length ? (
        <DataTable
          columns={columns}
          data={rows}
          label="Recent Demo Runs"
          onRowClick={(run) => onSelect(run.runId)}
        />
      ) : null}
      {!loading && !error && !rows.length ? (
        <p className="demo-placeholder">{t("No Demo Runs have been persisted yet.")}</p>
      ) : null}
      {selectedRunId ? (
        <span className="sr-only">{t("Selected Run {id}", { id: selectedRunId })}</span>
      ) : null}
    </Card>
  );
}

function DemoMutationError({ error }: { error: unknown }) {
  const requestId = error instanceof ApiError ? error.failure?.requestId : undefined;
  return (
    <div className="demo-mutation-error" role="alert">
      <CircleAlert aria-hidden="true" size={15} />
      <span>
        {formatError(error)}
        {requestId ? (
          <>
            {" "}
            / Request ID <code>{requestId}</code>
          </>
        ) : null}
      </span>
    </div>
  );
}

function shortId(value: string): string {
  return value.length > 18 ? `${value.slice(0, 8)}...${value.slice(-6)}` : value;
}
