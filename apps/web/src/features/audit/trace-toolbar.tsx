import {
  AlertTriangle,
  Bot,
  BrainCircuit,
  CircleHelp,
  GanttChart,
  GitBranch,
  Maximize2,
  Network,
  Search,
  Server,
  Wrench,
} from "lucide-react";

import { useI18n } from "../../lib/i18n";
import { cn } from "../../components/ui";
import { traceVisualTypes, type TraceVisualType } from "./trace-visualization-model";

export type TraceTimelineZoom = "fit" | 1 | 2 | 4;
export type TraceVisualizationView = "flow" | "timeline";

const typeOptions = [
  { type: "agent", label: "Agent", Icon: Bot },
  { type: "llm", label: "LLM", Icon: BrainCircuit },
  { type: "mcp", label: "MCP", Icon: Server },
  { type: "tool", label: "Local tool", Icon: Wrench },
  { type: "peer", label: "A2A", Icon: Network },
  { type: "retriever", label: "Retriever", Icon: Search },
  { type: "unknown", label: "Unknown", Icon: CircleHelp },
] satisfies Array<{ type: TraceVisualType; label: string; Icon: typeof Bot }>;

export function TraceToolbar({
  activeTypes,
  errorsOnly,
  flowNodeLimit,
  view,
  zoom,
  visibleCount,
  totalCount,
  onActiveTypesChange,
  onErrorsOnlyChange,
  onFlowNodeLimitChange,
  onViewChange,
  onZoomChange,
}: {
  activeTypes: ReadonlySet<TraceVisualType>;
  errorsOnly: boolean;
  flowNodeLimit: number;
  view: TraceVisualizationView;
  zoom: TraceTimelineZoom;
  visibleCount: number;
  totalCount: number;
  onActiveTypesChange: (types: Set<TraceVisualType>) => void;
  onErrorsOnlyChange: (value: boolean) => void;
  onFlowNodeLimitChange: (value: number) => void;
  onViewChange: (view: TraceVisualizationView) => void;
  onZoomChange: (value: TraceTimelineZoom) => void;
}) {
  const { t } = useI18n();
  const toggleType = (type: TraceVisualType) => {
    const next = new Set(activeTypes);
    if (next.has(type)) next.delete(type);
    else next.add(type);
    onActiveTypesChange(next);
  };
  const allTypesActive = activeTypes.size === traceVisualTypes.length;

  return (
    <div className="trace-toolbar">
      <div aria-label={t("Trace view")} className="trace-toolbar__view" role="group">
        <button
          aria-pressed={view === "flow"}
          className={cn("trace-toolbar__segment", view === "flow" && "is-active")}
          onClick={() => onViewChange("flow")}
          type="button"
        >
          <GitBranch aria-hidden="true" size={15} />
          {t("Flow")}
        </button>
        <button
          aria-pressed={view === "timeline"}
          className={cn("trace-toolbar__segment", view === "timeline" && "is-active")}
          onClick={() => onViewChange("timeline")}
          type="button"
        >
          <GanttChart aria-hidden="true" size={15} />
          {t("Timeline")}
        </button>
      </div>
      {view === "timeline" ? (
        <div aria-label={t("Span type filters")} className="trace-toolbar__types" role="group">
          <button
            aria-pressed={allTypesActive}
            className={cn("trace-toolbar__type", allTypesActive && "is-active")}
            onClick={() => onActiveTypesChange(new Set(traceVisualTypes))}
            type="button"
          >
            {t("All")}
          </button>
          {typeOptions.map(({ type, label, Icon }) => (
            <button
              aria-label={t("Toggle {type} spans", { type: t(label) })}
              aria-pressed={activeTypes.has(type)}
              className={cn(
                "trace-toolbar__type",
                `trace-toolbar__type--${type}`,
                activeTypes.has(type) && "is-active",
              )}
              key={type}
              onClick={() => toggleType(type)}
              title={t(label)}
              type="button"
            >
              <Icon aria-hidden="true" size={13} />
              <span>{t(label)}</span>
            </button>
          ))}
        </div>
      ) : (
        <div className="trace-toolbar__flow-spacer" />
      )}
      {view === "timeline" ? (
        <div className="trace-toolbar__actions">
          <span className="trace-toolbar__count">
            {t("{visible} of {total} spans", { visible: visibleCount, total: totalCount })}
          </span>
          <button
            aria-pressed={errorsOnly}
            className={cn("trace-toolbar__error", errorsOnly && "is-active")}
            onClick={() => onErrorsOnlyChange(!errorsOnly)}
            type="button"
          >
            <AlertTriangle aria-hidden="true" size={14} />
            {t("Errors only")}
          </button>
          <div aria-label={t("Timeline zoom")} className="trace-toolbar__zoom" role="group">
            {(["fit", 1, 2, 4] as const).map((value) => (
              <button
                aria-label={
                  value === "fit" ? t("Fit timeline") : t("{zoom}× zoom", { zoom: value })
                }
                aria-pressed={zoom === value}
                className={cn(zoom === value && "is-active")}
                key={value}
                onClick={() => onZoomChange(value)}
                title={value === "fit" ? t("Fit") : `${value}×`}
                type="button"
              >
                {value === "fit" ? <Maximize2 aria-hidden="true" size={13} /> : `${value}×`}
              </button>
            ))}
          </div>
        </div>
      ) : (
        <label className="trace-toolbar__node-limit">
          <span>{t("Nodes")}</span>
          <select
            aria-label={t("TraceFlow node limit")}
            onChange={(event) => onFlowNodeLimitChange(Number(event.target.value))}
            value={flowNodeLimit}
          >
            <option value="24">24</option>
            <option value="48">48</option>
            <option value="72">72</option>
          </select>
        </label>
      )}
    </div>
  );
}
