import { useReducedMotion } from "motion/react";
import {
  AlertCircle,
  Bot,
  BrainCircuit,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Circle,
  CircleHelp,
  ClockAlert,
  LoaderCircle,
  Network,
  Search,
  Server,
  Wrench,
} from "lucide-react";
import {
  Fragment,
  type CSSProperties,
  type ComponentType,
  type SVGProps,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import { cn } from "../../components/ui";
import { formatCount, formatTraceDuration } from "../../lib/format";
import { useI18n } from "../../lib/i18n";
import {
  buildTraceTimelineTicks,
  getTraceFocusIds,
  traceTimelineBarGeometry,
  type TraceVisualizationModel,
  type TraceVisualNode,
  type TraceVisualStatus,
  type TraceVisualType,
} from "./trace-visualization-model";
import type { TraceTimelineZoom } from "./trace-toolbar";

type TraceIcon = ComponentType<SVGProps<SVGSVGElement> & { size?: number | string }>;

const typePresentation: Record<TraceVisualType, { label: string; Icon: TraceIcon }> = {
  agent: { label: "Agent", Icon: Bot },
  llm: { label: "LLM", Icon: BrainCircuit },
  mcp: { label: "MCP", Icon: Server },
  tool: { label: "Local tool", Icon: Wrench },
  peer: { label: "A2A", Icon: Network },
  retriever: { label: "Retriever", Icon: Search },
  unknown: { label: "Unknown", Icon: CircleHelp },
};

export function TraceTimeline({
  model,
  rowIds,
  collapsedIds,
  selectedSpanId,
  zoom,
  onCollapsedIdsChange,
  onSelectSpan,
}: {
  model: TraceVisualizationModel;
  rowIds: string[];
  collapsedIds: ReadonlySet<string>;
  selectedSpanId?: string;
  zoom: TraceTimelineZoom;
  onCollapsedIdsChange: (ids: Set<string>) => void;
  onSelectSpan: (spanId: string, trigger?: HTMLElement) => void;
}) {
  const { t } = useI18n();
  const reducedMotion = useReducedMotion();
  const ticks = useMemo(
    () => buildTraceTimelineTicks(model.totalDurationMs),
    [model.totalDurationMs],
  );
  const focusIds = useMemo(() => getTraceFocusIds(model, selectedSpanId), [model, selectedSpanId]);
  const selectedPath = useMemo(
    () => getSelectedPath(model, selectedSpanId),
    [model, selectedSpanId],
  );
  const selectedChildren = new Set(model.nodesById.get(selectedSpanId ?? "")?.children ?? []);
  const firstDetachedId = model.detachedRoots.find((spanId) => rowIds.includes(spanId));
  const rowRefs = useRef(new Map<string, HTMLButtonElement>());
  const previousIds = useRef(new Set(model.rows));
  const [newIds, setNewIds] = useState(new Set<string>());
  const gridSize = ticks[1] ? `${ticks[1].ratio * 100}%` : "100%";
  const timelineStyle = {
    "--trace-grid-size": gridSize,
    "--trace-zoom": zoom === "fit" ? 0 : zoom,
  } as CSSProperties;

  useEffect(() => {
    const nextIds = new Set(model.rows);
    const additions = reducedMotion
      ? []
      : [...nextIds].filter((spanId) => !previousIds.current.has(spanId));
    previousIds.current = nextIds;
    if (!additions.length) {
      setNewIds(new Set());
      return;
    }
    setNewIds(new Set(additions));
    const timeout = window.setTimeout(() => setNewIds(new Set()), 240);
    return () => window.clearTimeout(timeout);
  }, [model.rows, reducedMotion]);

  useEffect(() => {
    if (!selectedSpanId) return;
    const selected = rowRefs.current.get(selectedSpanId);
    if (!selected || typeof selected.scrollIntoView !== "function") return;
    selected.scrollIntoView({
      block: "nearest",
      inline: "nearest",
      behavior: reducedMotion ? "auto" : "smooth",
    });
  }, [reducedMotion, selectedSpanId]);

  const toggleCollapsed = (spanId: string) => {
    const next = new Set(collapsedIds);
    if (next.has(spanId)) next.delete(spanId);
    else next.add(spanId);
    onCollapsedIdsChange(next);
  };

  return (
    <div className="trace-timeline" data-row-count={rowIds.length} style={timelineStyle}>
      <div
        aria-label={t("Trace timeline")}
        className={cn(
          "trace-timeline__scroll",
          `trace-timeline__scroll--${zoom === "fit" ? "fit" : `${zoom}x`}`,
        )}
        role="region"
        tabIndex={0}
      >
        <div className="trace-timeline__content">
          <header className="trace-timeline__header">
            <div className="trace-timeline__label-header">
              <span>{t("Span / component")}</span>
              <small>{t("Type · target · duration")}</small>
            </div>
            <div className="trace-timeline__scale">
              {ticks.map((tick) => (
                <span
                  className="trace-timeline__tick"
                  key={tick.elapsedMs}
                  style={{ left: `${tick.ratio * 100}%` }}
                >
                  <i aria-hidden="true" />
                  <small>{tick.label}</small>
                </span>
              ))}
            </div>
          </header>
          <div className="trace-timeline__body">
            {rowIds.length ? (
              rowIds.map((spanId) => {
                const node = model.nodesById.get(spanId);
                if (!node) return null;
                const presentation = typePresentation[node.type];
                const geometry = traceTimelineBarGeometry(node, model);
                const collapsed = collapsedIds.has(node.id);
                const dimmed = Boolean(selectedSpanId) && !focusIds.has(node.id);
                const isSelected = selectedSpanId === node.id;
                const isPath = selectedPath.has(node.id);
                const isDirectChild = selectedChildren.has(node.id);
                return (
                  <Fragment key={node.id}>
                    {node.id === firstDetachedId ? (
                      <div className="trace-timeline__group-row">
                        <span>{t("Detached spans")}</span>
                        <small>{t("Missing or invalid parent relationship")}</small>
                      </div>
                    ) : null}
                    <div
                      className={cn(
                        "trace-timeline__row",
                        `trace-timeline__row--${node.type}`,
                        `trace-timeline__row--${node.status}`,
                        isSelected && "trace-timeline__row--selected",
                        isPath && "trace-timeline__row--path",
                        isDirectChild && "trace-timeline__row--direct-child",
                        dimmed && "trace-timeline__row--dimmed",
                        newIds.has(node.id) && "trace-timeline__row--new",
                      )}
                      data-span-id={node.id}
                    >
                      <div
                        className="trace-timeline__label"
                        style={{ paddingInlineStart: 12 + Math.min(node.depth, 8) * 18 }}
                      >
                        {node.children.length ? (
                          <button
                            aria-label={t(collapsed ? "Expand {name}" : "Collapse {name}", {
                              name: node.span.name,
                            })}
                            aria-expanded={!collapsed}
                            className="trace-timeline__collapse"
                            onClick={() => toggleCollapsed(node.id)}
                            title={t(collapsed ? "Expand subtree" : "Collapse subtree")}
                            type="button"
                          >
                            {collapsed ? (
                              <ChevronRight aria-hidden="true" size={14} />
                            ) : (
                              <ChevronDown aria-hidden="true" size={14} />
                            )}
                          </button>
                        ) : (
                          <span
                            aria-hidden="true"
                            className="trace-timeline__collapse-placeholder"
                          />
                        )}
                        <button
                          aria-label={t("Open span {name}", { name: node.span.name })}
                          className="trace-timeline__select"
                          onClick={(event) => onSelectSpan(node.id, event.currentTarget)}
                          ref={(element) => {
                            if (element) rowRefs.current.set(node.id, element);
                            else rowRefs.current.delete(node.id);
                          }}
                          type="button"
                        >
                          <span className="trace-timeline__type-icon" title={t(presentation.label)}>
                            <presentation.Icon aria-hidden="true" size={15} />
                          </span>
                          <span className="trace-timeline__copy">
                            <span className="trace-timeline__title-line">
                              <strong>{node.span.name}</strong>
                              {node.depth > 8 ? (
                                <small className="trace-timeline__depth">d{node.depth}</small>
                              ) : null}
                            </span>
                            <small>{spanMetadata(node, t)}</small>
                          </span>
                          <span className="trace-timeline__status">
                            <StatusIcon status={node.status} />
                            <span className="sr-only">{t(statusLabel(node.status))}</span>
                          </span>
                        </button>
                      </div>
                      <div className="trace-timeline__time-cell">
                        <button
                          aria-label={t("Open timeline bar for {name}", { name: node.span.name })}
                          className="trace-timeline__bar-hitbox"
                          onClick={(event) => onSelectSpan(node.id, event.currentTarget)}
                          type="button"
                        >
                          <span
                            className="trace-timeline__bar"
                            style={{
                              left: `${geometry.leftRatio * 100}%`,
                              width: `max(${geometry.minWidthPx}px, ${geometry.widthRatio * 100}%)`,
                            }}
                            title={`${node.span.name} · ${formatTraceDuration(
                              node.span.durationMs,
                              node.status,
                            )}`}
                          >
                            {node.isClockSkewed ? (
                              <ClockAlert aria-label={t("Clock skew")} size={12} />
                            ) : null}
                          </span>
                        </button>
                      </div>
                    </div>
                  </Fragment>
                );
              })
            ) : (
              <div className="trace-timeline__empty">
                <AlertCircle aria-hidden="true" size={16} />
                <span>{t("No spans match the current visualization filters.")}</span>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function spanMetadata(node: TraceVisualNode, t: ReturnType<typeof useI18n>["t"]): string {
  const span = node.span;
  const values: string[] = [t(typePresentation[node.type].label)];
  if (node.type === "llm") {
    values.push(...[span.provider, span.model].filter(isString));
    if (span.totalTokens !== null)
      values.push(t("{count} tokens", { count: formatCount(span.totalTokens) }));
  } else if (node.type === "mcp") {
    values.push(...[span.mcpServer, span.toolName].filter(isString));
  } else if (node.type === "tool") {
    values.push(...[span.toolName, span.toolKind].filter(isString));
  } else if (node.type === "peer") {
    values.push(...[span.peerAgentId].filter(isString));
  } else if (node.type === "agent") {
    values.push(...[span.agentId].filter(isString));
  } else if (node.type === "unknown") {
    values.push(...[span.instrumentationScope].filter(isString));
  }
  values.push(t(formatTraceDuration(span.durationMs, node.status)));
  if (node.isClockSkewed) values.push(t("Clock skew"));
  return values.join(" · ");
}

function getSelectedPath(model: TraceVisualizationModel, selectedSpanId?: string): Set<string> {
  const path = new Set<string>();
  let currentId = selectedSpanId;
  while (currentId && !path.has(currentId)) {
    path.add(currentId);
    currentId = model.nodesById.get(currentId)?.parentId;
  }
  return path;
}

function StatusIcon({ status }: { status: TraceVisualStatus }) {
  if (status === "success") return <CheckCircle2 aria-hidden="true" size={14} />;
  if (status === "error") return <AlertCircle aria-hidden="true" size={14} />;
  if (status === "running") return <LoaderCircle aria-hidden="true" size={14} />;
  return <Circle aria-hidden="true" size={13} />;
}

function statusLabel(status: TraceVisualStatus): string {
  if (status === "success") return "Succeeded";
  if (status === "error") return "Failed";
  if (status === "running") return "Running";
  return "Status unset";
}

function isString(value: string | undefined): value is string {
  return Boolean(value);
}
