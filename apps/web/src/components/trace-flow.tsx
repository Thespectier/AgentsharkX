import { useReducedMotion } from "motion/react";
import { Boxes, Bot, BrainCircuit, Network, Wrench } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import { useI18n } from "../lib/i18n";
import { cn } from "./ui";

export const TRACE_FLOW_NODE_LIMIT = 72;

export type TraceFlowSpan = {
  traceId: string;
  spanId: string;
  parentSpanId?: string;
  name: string;
  startedAt: string;
  statusCode: string;
  agentId?: string;
  openInferenceKind?: string;
  toolKind?: string;
  peerAgentId?: string;
};

export type TraceFlowLink = {
  spanId: string;
  linkedTraceId: string;
  linkedSpanId: string;
};

export type TraceFlowLane = "agent" | "llm" | "tools" | "peer";

type FlowNode = {
  id: string;
  label: string;
  lane: TraceFlowLane;
  span?: TraceFlowSpan;
  foldedCount?: number;
  x: number;
  y: number;
};

type FlowEdge = {
  id: string;
  kind: "parent" | "link";
  sourceId: string;
  targetId?: string;
  externalLabel?: string;
};

export type TraceFlowLayout = {
  nodes: FlowNode[];
  edges: FlowEdge[];
  hiddenCount: number;
  width: number;
  height: number;
};

const lanes: Array<{ id: TraceFlowLane; label: string }> = [
  { id: "agent", label: "Root agent" },
  { id: "llm", label: "LLM" },
  { id: "tools", label: "MCP / tools" },
  { id: "peer", label: "Peer agents" },
];

const nodeWidth = 154;
const nodeHeight = 56;
const laneTop = 46;
const laneHeight = 104;
const laneLabelWidth = 122;
const nodeGap = 22;

export function traceFlowLane(span: TraceFlowSpan): TraceFlowLane {
  if (span.peerAgentId) return "peer";
  const kind = span.openInferenceKind?.trim().toUpperCase();
  if (kind === "LLM") return "llm";
  if (kind === "TOOL" || span.toolKind) return "tools";
  return "agent";
}

export function buildTraceFlowLayout(
  spans: TraceFlowSpan[],
  links: TraceFlowLink[],
  selectedSpanId?: string,
  maxNodes = TRACE_FLOW_NODE_LIMIT,
): TraceFlowLayout {
  const ordered = [...spans].sort((left, right) => {
    const time = left.startedAt.localeCompare(right.startedAt);
    return time || left.spanId.localeCompare(right.spanId);
  });
  const foldedLaneCapacity = Math.min(lanes.length, Math.max(0, maxNodes - 1));
  const actualCapacity = Math.max(1, maxNodes - foldedLaneCapacity);
  const visible = ordered.slice(0, actualCapacity);
  if (selectedSpanId && !visible.some((span) => span.spanId === selectedSpanId)) {
    const selected = ordered.find((span) => span.spanId === selectedSpanId);
    if (selected) visible[Math.max(0, visible.length - 1)] = selected;
  }
  const visibleIDs = new Set(visible.map((span) => span.spanId));
  const hidden = ordered.filter((span) => !visibleIDs.has(span.spanId));
  const hiddenByLane = new Map<TraceFlowLane, number>();
  for (const span of hidden) {
    const lane = traceFlowLane(span);
    hiddenByLane.set(lane, (hiddenByLane.get(lane) ?? 0) + 1);
  }

  const unpositioned: Array<Omit<FlowNode, "x" | "y">> = visible.map((span) => ({
    id: span.spanId,
    label: span.name,
    lane: traceFlowLane(span),
    span,
  }));
  for (const { id, label } of lanes) {
    const foldedCount = hiddenByLane.get(id);
    if (foldedCount) {
      unpositioned.push({
        id: `folded:${id}`,
        label: `${foldedCount} folded`,
        lane: id,
        foldedCount,
      });
    }
  }

  const laneCounts = new Map<TraceFlowLane, number>();
  const nodes = unpositioned.map<FlowNode>((node) => {
    const index = laneCounts.get(node.lane) ?? 0;
    laneCounts.set(node.lane, index + 1);
    return {
      ...node,
      x: laneLabelWidth + index * (nodeWidth + nodeGap),
      y: laneTop + lanes.findIndex((lane) => lane.id === node.lane) * laneHeight,
    };
  });
  const nodeIDs = new Set(nodes.map((node) => node.id));
  const edges: FlowEdge[] = [];
  for (const span of visible) {
    if (span.parentSpanId && nodeIDs.has(span.parentSpanId)) {
      edges.push({
        id: `parent:${span.parentSpanId}:${span.spanId}`,
        kind: "parent",
        sourceId: span.parentSpanId,
        targetId: span.spanId,
      });
    }
  }
  for (const link of [...links].sort((left, right) =>
    `${left.spanId}:${left.linkedTraceId}:${left.linkedSpanId}`.localeCompare(
      `${right.spanId}:${right.linkedTraceId}:${right.linkedSpanId}`,
    ),
  )) {
    if (!nodeIDs.has(link.spanId)) continue;
    const sourceSpan = visible.find((span) => span.spanId === link.spanId);
    const localTarget =
      sourceSpan?.traceId === link.linkedTraceId && nodeIDs.has(link.linkedSpanId);
    edges.push({
      id: `link:${link.spanId}:${link.linkedTraceId}:${link.linkedSpanId}`,
      kind: "link",
      sourceId: link.spanId,
      targetId: localTarget ? link.linkedSpanId : undefined,
      externalLabel: localTarget
        ? undefined
        : `${shortID(link.linkedTraceId)} / ${shortID(link.linkedSpanId)}`,
    });
  }
  const mostPopulatedLane = Math.max(1, ...laneCounts.values());
  return {
    nodes,
    edges,
    hiddenCount: hidden.length,
    width: Math.max(900, laneLabelWidth + mostPopulatedLane * (nodeWidth + nodeGap) + 70),
    height: laneTop + lanes.length * laneHeight + 14,
  };
}

export function TraceFlow({
  spans,
  links,
  selectedSpanId,
  maxNodes = TRACE_FLOW_NODE_LIMIT,
  onSelect,
}: {
  spans: TraceFlowSpan[];
  links: TraceFlowLink[];
  selectedSpanId?: string;
  maxNodes?: number;
  onSelect: (spanId: string, trigger: HTMLButtonElement) => void;
}) {
  const { t } = useI18n();
  const reducedMotion = useReducedMotion();
  const layout = useMemo(
    () => buildTraceFlowLayout(spans, links, selectedSpanId, maxNodes),
    [links, maxNodes, selectedSpanId, spans],
  );
  const previousIDs = useRef(new Set(spans.map((span) => span.spanId)));
  const [newIDs, setNewIDs] = useState(new Set<string>());

  useEffect(() => {
    const next = new Set(spans.map((span) => span.spanId));
    const additions = reducedMotion
      ? []
      : [...next].filter((spanId) => !previousIDs.current.has(spanId));
    previousIDs.current = next;
    if (!additions.length) {
      setNewIDs(new Set());
      return;
    }
    setNewIDs(new Set(additions));
    const timeout = window.setTimeout(() => setNewIDs(new Set()), 1_600);
    return () => window.clearTimeout(timeout);
  }, [reducedMotion, spans]);

  const nodeByID = new Map(layout.nodes.map((node) => [node.id, node]));
  return (
    <div className="trace-flow" data-node-count={layout.nodes.length}>
      <div
        aria-label={t("Deterministic trace flow")}
        className="trace-flow__scroll"
        role="region"
        tabIndex={0}
      >
        <div
          aria-label={t("Trace flow lanes")}
          className="trace-flow__canvas"
          role="group"
          style={{ width: layout.width, height: layout.height }}
        >
          {lanes.map((lane, index) => (
            <div
              className="trace-flow__lane"
              key={lane.id}
              style={{ top: laneTop + index * laneHeight - 17 }}
            >
              <span>{t(lane.label)}</span>
            </div>
          ))}
          <svg aria-hidden="true" height={layout.height} width={layout.width}>
            {layout.edges.map((edge) => {
              const source = nodeByID.get(edge.sourceId);
              const target = edge.targetId ? nodeByID.get(edge.targetId) : undefined;
              if (!source) return null;
              const sourceX = source.x + nodeWidth;
              const sourceY = source.y + nodeHeight / 2;
              const targetX = target ? target.x : sourceX + 54;
              const targetY = target ? target.y + nodeHeight / 2 : sourceY;
              const midX = sourceX + (targetX - sourceX) / 2;
              return (
                <g key={edge.id}>
                  <path
                    className={cn("trace-flow__edge", `trace-flow__edge--${edge.kind}`)}
                    d={`M ${sourceX} ${sourceY} C ${midX} ${sourceY}, ${midX} ${targetY}, ${targetX} ${targetY}`}
                    data-edge-kind={edge.kind}
                  />
                  {edge.externalLabel ? (
                    <text className="trace-flow__external-label" x={targetX + 5} y={targetY + 3}>
                      {edge.externalLabel}
                    </text>
                  ) : null}
                </g>
              );
            })}
          </svg>
          {layout.nodes.map((node) => {
            if (!node.span) {
              return (
                <div
                  aria-label={`${node.foldedCount} ${node.lane} spans folded`}
                  className="trace-flow__folded"
                  key={node.id}
                  style={{ left: node.x, top: node.y }}
                >
                  <Boxes size={15} />
                  <span>{node.label}</span>
                </div>
              );
            }
            const Icon = laneIcon(node.lane);
            return (
              <button
                aria-label={`Open span ${node.span.name}`}
                className={cn(
                  "trace-flow__node",
                  `trace-flow__node--${node.lane}`,
                  node.span.statusCode === "error" && "trace-flow__node--error",
                  selectedSpanId === node.id && "trace-flow__node--selected",
                  newIDs.has(node.id) && "trace-flow__node--new",
                )}
                key={node.id}
                onClick={(event) => onSelect(node.id, event.currentTarget)}
                style={{ left: node.x, top: node.y }}
                title={`${node.span.name} · ${node.span.spanId}`}
              >
                <Icon aria-hidden="true" size={15} />
                <span>
                  <strong>{node.label}</strong>
                  <small>{shortID(node.id)}</small>
                </span>
              </button>
            );
          })}
        </div>
      </div>
      <footer className="trace-flow__legend">
        <span>
          <i className="trace-flow__legend-line" /> {t("Explicit parent")}
        </span>
        <span>
          <i className="trace-flow__legend-line trace-flow__legend-line--link" /> {t("Span Link")}
        </span>
        {layout.hiddenCount ? (
          <strong>{t("{count} spans folded for clarity", { count: layout.hiddenCount })}</strong>
        ) : null}
      </footer>
    </div>
  );
}

function laneIcon(lane: TraceFlowLane) {
  if (lane === "llm") return BrainCircuit;
  if (lane === "tools") return Wrench;
  if (lane === "peer") return Network;
  return Bot;
}

function shortID(value: string): string {
  return value.length > 12 ? `${value.slice(0, 6)}…${value.slice(-4)}` : value;
}
