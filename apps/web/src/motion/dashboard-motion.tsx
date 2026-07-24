import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { Activity, Bot, Box, BrainCircuit, Radio, ShieldCheck } from "lucide-react";
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
import type { TrendPoint, UnifiedEvent } from "../types";
import { SeverityBadge, SourceBadge, StatusOrb, cn } from "../components/ui";

const flowPaths = [
  "M 122 112 C 205 112, 202 58, 296 58",
  "M 122 112 C 205 112, 202 164, 296 164",
  "M 420 58 C 496 58, 502 38, 590 38",
  "M 420 58 C 496 58, 502 92, 590 92",
  "M 420 164 C 496 164, 502 145, 590 145",
  "M 420 164 C 496 164, 502 198, 590 198",
];

export function LiveFlow({
  events,
  status,
}: {
  events: UnifiedEvent[];
  status: "connecting" | "live" | "paused";
}) {
  const reduced = useReducedMotion();
  const { t } = useI18n();
  const pulses = useMemo(() => events.slice(0, 12), [events]);
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
        {flowPaths.map((path) => (
          <path className="live-flow__path" d={path} key={path} />
        ))}
        <FlowNode icon={<Bot size={20} />} label="Agents" meta="31 explicit IDs" x={28} y={82} />
        <FlowNode
          icon={<BrainCircuit size={20} />}
          label="Gateway"
          meta="4 listeners"
          x={296}
          y={28}
        />
        <FlowNode
          icon={<ShieldCheck size={20} />}
          label="Guard"
          meta="8 active rules"
          x={296}
          y={134}
        />
        <FlowNode
          icon={<Activity size={18} />}
          label="LLM"
          meta="3 providers"
          small
          x={590}
          y={12}
        />
        <FlowNode icon={<Box size={18} />} label="MCP" meta="3 servers" small x={590} y={66} />
        <FlowNode icon={<Bot size={18} />} label="A2A" meta="2 routes" small x={590} y={119} />
        <FlowNode
          icon={<ShieldCheck size={18} />}
          label="Review"
          meta="3 pending"
          small
          x={590}
          y={172}
        />
        {!reduced && status === "live"
          ? pulses.map((event, index) => {
              const path = flowPaths[index % flowPaths.length];
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
                    path={path}
                    repeatCount="1"
                  />
                </circle>
              );
            })
          : null}
      </svg>
      <div className="live-flow__footer">
        <span>
          <i className="legend-dot legend-dot--blue" />
          {t("Gateway traffic")}
        </span>
        <span>
          <i className="legend-dot legend-dot--cyan" />
          {t("Guard decisions")}
        </span>
        <span>{t("Resumable SSE · no inferred correlation")}</span>
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
  meta: string;
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
            <small>{t(meta)}</small>
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
