import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { Activity, BrainCircuit, CircleAlert, Radio, ShieldCheck, ShieldX } from "lucide-react";
import { useMemo } from "react";
import {
  Area,
  AreaChart,
  CartesianGrid,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import {
  displayTimeZoneLabel,
  formatCount,
  formatDuration,
  formatTimeWithZone,
  formatTrendTick,
  formatTrendTimestamp,
} from "../lib/format";
import { useI18n } from "../lib/i18n";
import type { Metric, SourceHealth, TrendPoint, UnifiedEvent } from "../types";
import { SeverityBadge, SourceBadge, StatusOrb, cn } from "../components/ui";

const flowPaths = {
  gatewayInput: "M 122 112 C 205 112, 202 58, 296 58",
  guardInput: "M 122 112 C 205 112, 202 164, 296 164",
  gatewayRequest: "M 420 58 C 496 58, 502 38, 590 38",
  gatewayError: "M 420 58 C 496 58, 502 92, 590 92",
  guardDecision: "M 420 164 C 496 164, 502 145, 590 145",
  guardDenied: "M 420 164 C 496 164, 502 198, 590 198",
} as const;

export type LiveFlowCategory =
  "gateway-request" | "gateway-error" | "guard-decision" | "guard-denied";

export function classifyLiveFlowEvent(event: UnifiedEvent): LiveFlowCategory | undefined {
  if (event.kind === "health") return undefined;
  if (event.source === "agentgateway") {
    return event.severity === "high" || event.severity === "critical"
      ? "gateway-error"
      : "gateway-request";
  }
  const explicitDecision = (event.decision ?? event.action ?? "").trim().toUpperCase();
  return explicitDecision === "DENY" ? "guard-denied" : "guard-decision";
}

function categoryPath(category: LiveFlowCategory): string {
  switch (category) {
    case "gateway-error":
      return flowPaths.gatewayError;
    case "guard-decision":
      return flowPaths.guardDecision;
    case "guard-denied":
      return flowPaths.guardDenied;
    default:
      return flowPaths.gatewayRequest;
  }
}

export function summarizeLiveFlow(metrics: Metric[], trend: TrendPoint[]) {
  const metricValue = (...ids: string[]) => {
    for (const id of ids) {
      const metric = metrics.find((item) => item.id === id);
      if (metric) return Math.max(0, metric.value);
    }
    return undefined;
  };
  const total = (field: "requests" | "errors" | "denied") =>
    trend.reduce((sum, point) => sum + Math.max(0, point[field]), 0);
  return {
    gatewayRequests: metricValue("gateway-requests", "requests") ?? total("requests"),
    gatewayErrors: total("errors"),
    guardDecisions: metricValue("guard-decisions") ?? 0,
    guardDenied: total("denied"),
  };
}

export function LiveFlow({
  events,
  health,
  metrics,
  status,
  trend,
}: {
  events: UnifiedEvent[];
  health: Array<Pick<SourceHealth, "source" | "status">>;
  metrics: Metric[];
  status: "connecting" | "live" | "paused";
  trend: TrendPoint[];
}) {
  const reduced = useReducedMotion();
  const { t } = useI18n();
  const pulses = useMemo(
    () =>
      events
        .map((event) => ({ event, category: classifyLiveFlowEvent(event) }))
        .filter(
          (item): item is { event: UnifiedEvent; category: LiveFlowCategory } =>
            item.category !== undefined,
        )
        .slice(0, 12),
    [events],
  );
  const summary = summarizeLiveFlow(metrics, trend);
  const sourceStatus = (source: SourceHealth["source"]) =>
    health.find((item) => item.source === source)?.status ?? "unknown";
  const windowCount = (value: number) => `${formatCount(value)} · ${t("Last 60m")}`;
  return (
    <div
      className="live-flow"
      data-motion={reduced ? "reduced" : status === "paused" ? "paused" : "full"}
    >
      <div className="live-flow__header">
        <div>
          <span className="live-flow__label">
            <Radio size={13} /> {t("Live control plane")}
          </span>
          <strong>{t("Agent traffic & decisions")}</strong>
        </div>
        <span className="live-flow__state">
          <StatusOrb
            status={
              status === "live" ? "healthy" : status === "connecting" ? "connecting" : "degraded"
            }
          />
          {t(status)}
        </span>
      </div>
      <svg aria-label={t("Live agent traffic topology")} role="img" viewBox="0 0 712 236">
        <defs>
          <linearGradient id="flow-blue" x1="0" x2="1">
            <stop offset="0" stopColor="#5c92ff" stopOpacity="0.2" />
            <stop offset="1" stopColor="#32d6e8" stopOpacity="0.75" />
          </linearGradient>
          <filter id="flow-glow">
            <feGaussianBlur result="blur" stdDeviation="3" />
            <feMerge>
              <feMergeNode in="blur" />
              <feMergeNode in="SourceGraphic" />
            </feMerge>
          </filter>
        </defs>
        {Object.values(flowPaths).map((path) => (
          <path className="live-flow__path" d={path} key={path} />
        ))}
        <FlowNode
          icon={<Activity size={20} />}
          label="Activity"
          meta={`${formatCount(pulses.length)} · ${t("recent events")}`}
          x={28}
          y={82}
        />
        <FlowNode
          icon={<BrainCircuit size={20} />}
          label="Gateway"
          meta={t(sourceStatus("agentgateway"))}
          x={296}
          y={28}
        />
        <FlowNode
          icon={<ShieldCheck size={20} />}
          label="Guard"
          meta={t(sourceStatus("agentguard"))}
          x={296}
          y={134}
        />
        <FlowNode
          icon={<Activity size={18} />}
          label="Requests"
          meta={windowCount(summary.gatewayRequests)}
          small
          x={590}
          y={12}
        />
        <FlowNode
          icon={<CircleAlert size={18} />}
          label="Errors"
          meta={windowCount(summary.gatewayErrors)}
          small
          x={590}
          y={66}
        />
        <FlowNode
          icon={<ShieldCheck size={18} />}
          label="Decisions"
          meta={windowCount(summary.guardDecisions)}
          small
          x={590}
          y={119}
        />
        <FlowNode
          icon={<ShieldX size={18} />}
          label="Denied"
          meta={windowCount(summary.guardDenied)}
          small
          x={590}
          y={172}
        />
        {!reduced && status === "live"
          ? pulses.map(({ event, category }, index) => {
              return (
                <circle
                  className={cn(
                    "live-flow__particle",
                    event.source === "agentguard" && "live-flow__particle--guard",
                  )}
                  key={event.id}
                  r="3.1"
                >
                  <animateMotion
                    begin={`${(index % 4) * 0.16}s`}
                    dur="1.35s"
                    fill="freeze"
                    path={categoryPath(category)}
                    repeatCount="1"
                  />
                </circle>
              );
            })
          : null}
      </svg>
      <div
        aria-label={t("Live agent traffic summary")}
        className="live-flow__mobile-grid"
        role="group"
      >
        <span className="live-flow__mobile-count">
          {formatCount(pulses.length)} {t("recent events")}
        </span>
        <div className="live-flow__mobile-source">
          <div className="live-flow__mobile-identity">
            <BrainCircuit size={18} />
            <span>
              <strong>{t("Gateway")}</strong>
              <small>{t(sourceStatus("agentgateway"))}</small>
            </span>
          </div>
          <dl>
            <div>
              <dt>{t("Requests")}</dt>
              <dd>{windowCount(summary.gatewayRequests)}</dd>
            </div>
            <div>
              <dt>{t("Errors")}</dt>
              <dd>{windowCount(summary.gatewayErrors)}</dd>
            </div>
          </dl>
        </div>
        <div className="live-flow__mobile-source">
          <div className="live-flow__mobile-identity">
            <ShieldCheck size={18} />
            <span>
              <strong>{t("Guard")}</strong>
              <small>{t(sourceStatus("agentguard"))}</small>
            </span>
          </div>
          <dl>
            <div>
              <dt>{t("Decisions")}</dt>
              <dd>{windowCount(summary.guardDecisions)}</dd>
            </div>
            <div>
              <dt>{t("Denied")}</dt>
              <dd>{windowCount(summary.guardDenied)}</dd>
            </div>
          </dl>
        </div>
      </div>
      <div className="live-flow__footer">
        <span>
          <i className="legend-dot legend-dot--blue" />
          {t("Gateway traffic")}
        </span>
        <span>
          <i className="legend-dot legend-dot--cyan" />
          {t("AgentGuard activity")}
        </span>
        <span>{t("Rolling 60 minutes · source-preserved")}</span>
      </div>
    </div>
  );
}

function FlowNode({
  x,
  y,
  label,
  meta,
  icon,
  small = false,
}: {
  x: number;
  y: number;
  label: string;
  meta: React.ReactNode;
  icon: React.ReactNode;
  small?: boolean;
}) {
  const { t } = useI18n();
  return (
    <g className={cn("flow-node", small && "flow-node--small")} transform={`translate(${x} ${y})`}>
      <rect height={small ? 48 : 60} rx={small ? 10 : 13} width={small ? 104 : 124} />
      <foreignObject height={small ? 48 : 60} width={small ? 104 : 124}>
        <div className="flow-node__content">
          {icon}
          <span>
            <strong>{t(label)}</strong>
            <small>{meta}</small>
          </span>
        </div>
      </foreignObject>
    </g>
  );
}

export function ActivityRail({ events, limit = 6 }: { events: UnifiedEvent[]; limit?: number }) {
  const reduced = useReducedMotion();
  const { t } = useI18n();
  return (
    <div className="activity-rail">
      <AnimatePresence initial={false}>
        {events.slice(0, limit).map((event, index) => (
          <motion.article
            className={cn("activity-item", index === 0 && "activity-item--new")}
            initial={reduced ? false : { opacity: 0, y: -10 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, height: 0 }}
            key={event.id}
            layout={!reduced}
            transition={{ duration: 0.28 }}
          >
            <span className={cn("activity-item__line", `activity-item__line--${event.severity}`)} />
            <div className="activity-item__body">
              <div className="activity-item__meta">
                <SourceBadge source={event.source} />
                <span>{formatTimeWithZone(event.timestamp)}</span>
              </div>
              <p>{event.summary}</p>
              <div className="activity-item__footer">
                <span>{t(event.phase ?? event.kind)}</span>
                {event.decision ? (
                  <strong>{t(event.decision)}</strong>
                ) : (
                  <SeverityBadge severity={event.severity} />
                )}
              </div>
            </div>
          </motion.article>
        ))}
      </AnimatePresence>
    </div>
  );
}

export function RequestTrendChart({
  data,
  mode = "requests",
}: {
  data: TrendPoint[];
  mode?: "requests" | "latency" | "security";
}) {
  const reduced = useReducedMotion();
  const { locale, t } = useI18n();
  const primaryKey = mode === "latency" ? "latency" : mode === "security" ? "denied" : "requests";
  const color = mode === "security" ? "#ff627d" : mode === "latency" ? "#32d6e8" : "#5c92ff";
  const primaryAxis = mode === "requests" ? "traffic" : "primary";
  const primaryName = t(
    mode === "latency" ? "P95 latency" : mode === "security" ? "Denied" : "Requests",
  );
  return (
    <div
      className="chart-wrap"
      role="img"
      aria-label={t(
        "{mode} trend chart for the last 60 minutes in {count} five-minute Beijing-time buckets",
        { mode: t(mode), count: data.length },
      )}
    >
      <ResponsiveContainer height="100%" width="100%">
        <AreaChart data={data} margin={{ left: 0, right: 0, top: 12, bottom: 0 }}>
          <defs>
            <linearGradient id={`chart-${mode}`} x1="0" x2="0" y1="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity={0.28} />
              <stop offset="100%" stopColor={color} stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid stroke="#1c2a3a" strokeDasharray="3 5" vertical={false} />
          <XAxis
            axisLine={false}
            dataKey="time"
            fontSize={11}
            interval={1}
            stroke="#718196"
            tickFormatter={formatTrendTick}
            tickLine={false}
          />
          <YAxis
            allowDecimals={false}
            axisLine={false}
            domain={[0, "auto"]}
            fontSize={11}
            stroke="#718196"
            tickFormatter={(value: number) =>
              mode === "latency" ? `${formatCount(value)} ms` : formatCount(value)
            }
            tickLine={false}
            width={mode === "latency" ? 58 : 45}
            yAxisId={primaryAxis}
          />
          {mode === "requests" ? (
            <YAxis
              allowDecimals={false}
              axisLine={false}
              domain={[0, "auto"]}
              fontSize={11}
              orientation="right"
              stroke="#a56b78"
              tickFormatter={formatCount}
              tickLine={false}
              width={38}
              yAxisId="decisions"
            />
          ) : null}
          <Tooltip
            contentStyle={{
              background: "#101927",
              border: "1px solid #26384d",
              borderRadius: 10,
              color: "#eef5ff",
              fontSize: 12,
            }}
            cursor={{ stroke: "#41536a" }}
            filterNull={false}
            formatter={(value, name) => [
              value == null
                ? t("No request samples")
                : mode === "latency"
                  ? formatDuration(Number(value))
                  : formatCount(Number(value)),
              String(name),
            ]}
            labelFormatter={(value, payload) => {
              const point = payload[0]?.payload as TrendPoint | undefined;
              const sampleLabel =
                mode === "latency"
                  ? ` · ${formatCount(point?.latencySamples ?? 0)} ${t("samples")}`
                  : "";
              return `${formatTrendTimestamp(
                String(value),
                locale,
              )} ${displayTimeZoneLabel} · ${t("5-minute bucket")}${sampleLabel}`;
            }}
          />
          <Area
            animationDuration={reduced ? 0 : 650}
            connectNulls={false}
            dataKey={primaryKey}
            fill={`url(#chart-${mode})`}
            name={primaryName}
            stroke={color}
            strokeWidth={2}
            type="monotone"
            yAxisId={primaryAxis}
          />
          {mode === "requests" ? (
            <Line
              animationDuration={reduced ? 0 : 650}
              dataKey="denied"
              dot={false}
              name={t("Denied")}
              stroke="#ff627d"
              strokeDasharray="4 4"
              strokeWidth={1.5}
              type="monotone"
              yAxisId="decisions"
            />
          ) : null}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
