import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import {
  AlertCircle,
  ArrowDownRight,
  ArrowUpRight,
  Check,
  ChevronRight,
  CircleSlash,
  Clock3,
  DatabaseZap,
  ExternalLink,
  Inbox,
  LoaderCircle,
  Minus,
  RefreshCw,
  ShieldAlert,
  X,
} from "lucide-react";
import {
  type KeyboardEvent,
  type ReactNode,
  type RefObject,
  forwardRef,
  useEffect,
  useId,
  useRef,
} from "react";

import { formatMetric, sourceLabel } from "../lib/format";
import { localizeChildren, useI18n } from "../lib/i18n";
import type { HealthStatus, Metric, ResponseMeta, Severity, Source } from "../types";

export function cn(...values: Array<string | false | null | undefined>) {
  return values.filter(Boolean).join(" ");
}

export function Card({
  children,
  className,
  elevated = false,
  as = "section",
}: {
  children: ReactNode;
  className?: string;
  elevated?: boolean;
  as?: "section" | "article" | "div";
}) {
  const Component = as;
  return (
    <Component className={cn("panel", elevated && "panel--elevated", className)}>
      {children}
    </Component>
  );
}

export function CardHeader({
  title,
  description,
  action,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  const { t } = useI18n();
  return (
    <header className="card-header">
      <div>
        <h2>{t(title)}</h2>
        {description ? <p>{t(description)}</p> : null}
      </div>
      {action ? <div className="card-header__action">{action}</div> : null}
    </header>
  );
}

export function PageHeader({
  eyebrow,
  title,
  description,
  actions,
  children,
}: {
  eyebrow: string;
  title: string;
  description: string;
  actions?: ReactNode;
  children?: ReactNode;
}) {
  const { t } = useI18n();
  return (
    <header className="page-header">
      <div className="page-header__copy">
        <p className="eyebrow">{t(eyebrow)}</p>
        <h1>{t(title)}</h1>
        <p className="page-header__description">{t(description)}</p>
      </div>
      {actions ? <div className="page-header__actions">{actions}</div> : null}
      {children ? <div className="page-header__footer">{children}</div> : null}
    </header>
  );
}

type ButtonProps = React.ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "ghost" | "danger";
  size?: "sm" | "md";
};

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { children, variant = "secondary", size = "md", className, ...props },
  ref,
) {
  const { t } = useI18n();
  return (
    <button
      className={cn("button", `button--${variant}`, `button--${size}`, className)}
      ref={ref}
      {...props}
    >
      {localizeChildren(children, t)}
    </button>
  );
});

export function ExternalButton({ href, children }: { href: string; children: ReactNode }) {
  const { t } = useI18n();
  return (
    <a className="button button--secondary button--md" href={href} rel="noreferrer" target="_blank">
      {localizeChildren(children, t)}
      <ExternalLink aria-hidden="true" size={14} />
    </a>
  );
}

export function StatusOrb({ status, label }: { status: HealthStatus; label?: string }) {
  const { t } = useI18n();
  const localizedLabel = t(label ?? status);
  return (
    <span className="status-orb-wrap" title={localizedLabel}>
      <span aria-hidden="true" className={cn("status-orb", `status-orb--${status}`)} />
      <span className="sr-only">{localizedLabel}</span>
    </span>
  );
}

export function SourceBadge({ source }: { source: Source }) {
  return (
    <span className={cn("source-badge", `source-badge--${source}`)}>
      <span aria-hidden="true" className="source-badge__mark" />
      {sourceLabel(source)}
    </span>
  );
}

export function StatusBadge({ status }: { status: string }) {
  const { t } = useI18n();
  const normalized = status.toLowerCase().replaceAll(" ", "-");
  return <span className={cn("status-badge", `status-badge--${normalized}`)}>{t(status)}</span>;
}

export function SeverityBadge({ severity }: { severity: Severity }) {
  const { t } = useI18n();
  return (
    <span className={cn("severity-badge", `severity-badge--${severity}`)}>
      <ShieldAlert aria-hidden="true" size={12} />
      {t(severity)}
    </span>
  );
}

export function MetricTicker({ metric }: { metric: Pick<Metric, "value" | "format"> }) {
  const reduced = useReducedMotion();
  const display = formatMetric(metric);
  return (
    <span className="metric-ticker" aria-label={display}>
      <AnimatePresence initial={false} mode="popLayout">
        <motion.span
          key={display}
          initial={reduced ? false : { opacity: 0.35, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          exit={reduced ? undefined : { opacity: 0, y: -8 }}
          transition={{ duration: 0.24, ease: "easeOut" }}
        >
          {display}
        </motion.span>
      </AnimatePresence>
    </span>
  );
}

export function MetricCard({ metric }: { metric: Metric }) {
  const { t } = useI18n();
  const TrendIcon =
    metric.trend === "up" ? ArrowUpRight : metric.trend === "down" ? ArrowDownRight : Minus;
  return (
    <Card className={cn("metric-card", `metric-card--${metric.tone}`)} as="article">
      <div className="metric-card__topline">
        <span className="metric-card__label">
          {t(metric.label)}
          <SourceBadge source={metric.source} />
        </span>
        <span className={cn("metric-delta", `metric-delta--${metric.trend}`)}>
          <TrendIcon aria-hidden="true" size={13} />
          {metric.delta === 0 ? t("steady") : `${Math.abs(metric.delta).toFixed(1)}%`}
        </span>
      </div>
      <MetricTicker metric={metric} />
      <p>{t(metric.context)}</p>
    </Card>
  );
}

export function PartialBanner({ meta }: { meta?: ResponseMeta }) {
  const { t } = useI18n();
  if (!meta?.partial || !meta.sourceFailures?.length) return null;
  return (
    <div className="partial-banner" role="status">
      <DatabaseZap aria-hidden="true" size={18} />
      <div>
        <strong>{t("Partial data")}</strong>
        <span>
          {meta.sourceFailures
            .map((failure) => `${sourceLabel(failure.source)}: ${failure.message}`)
            .join(" · ")}
        </span>
      </div>
    </div>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return <span aria-hidden="true" className={cn("skeleton", className)} />;
}

export function PageSkeleton({ label = "Loading console data" }: { label?: string }) {
  const { t } = useI18n();
  return (
    <div aria-busy="true" aria-label={t(label)} className="page-skeleton" role="status">
      <div className="skeleton-heading">
        <Skeleton className="skeleton--eyebrow" />
        <Skeleton className="skeleton--title" />
        <Skeleton className="skeleton--copy" />
      </div>
      <div className="metric-grid">
        {[0, 1, 2, 3].map((key) => (
          <Card className="skeleton-card" key={key}>
            <Skeleton className="skeleton--label" />
            <Skeleton className="skeleton--value" />
            <Skeleton className="skeleton--copy" />
          </Card>
        ))}
      </div>
      <div className="content-grid content-grid--wide">
        <Card className="skeleton-panel">
          <Skeleton className="skeleton--chart" />
        </Card>
        <Card className="skeleton-panel">
          <Skeleton className="skeleton--chart" />
        </Card>
      </div>
    </div>
  );
}

export function EmptyState({
  title,
  description,
  action,
  compact = false,
}: {
  title: string;
  description: string;
  action?: ReactNode;
  compact?: boolean;
}) {
  const { t } = useI18n();
  return (
    <div className={cn("state-card", compact && "state-card--compact")}>
      <span className="state-card__icon">
        <Inbox aria-hidden="true" size={22} />
      </span>
      <h2>{t(title)}</h2>
      <p>{t(description)}</p>
      {action ? <div className="state-card__action">{action}</div> : null}
    </div>
  );
}

export function ErrorState({
  title = "This view could not be loaded",
  description,
  onRetry,
}: {
  title?: string;
  description: string;
  onRetry?: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className="state-card state-card--error" role="alert">
      <span className="state-card__icon">
        <AlertCircle aria-hidden="true" size={22} />
      </span>
      <h2>{t(title)}</h2>
      <p>{t(description)}</p>
      {onRetry ? (
        <Button onClick={onRetry} variant="secondary">
          <RefreshCw aria-hidden="true" size={14} /> Retry
        </Button>
      ) : null}
    </div>
  );
}

export interface Column<T> {
  key: string;
  header: string;
  className?: string;
  render: (item: T) => ReactNode;
}

export function DataTable<T extends { id: string }>({
  columns,
  data,
  label,
  onRowClick,
}: {
  columns: Column<T>[];
  data: T[];
  label: string;
  onRowClick?: (item: T, trigger: HTMLTableRowElement) => void;
}) {
  const { t } = useI18n();
  const handleKey = (event: KeyboardEvent<HTMLTableRowElement>, item: T) => {
    if (!onRowClick) return;
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onRowClick(item, event.currentTarget);
    }
  };
  return (
    <div
      aria-label={`${t(label)} ${t("scroll region")}`}
      className="table-scroll"
      role="region"
      tabIndex={0}
    >
      <table className="data-table">
        <caption className="sr-only">{t(label)}</caption>
        <thead>
          <tr>
            {columns.map((column) => (
              <th className={column.className} key={column.key} scope="col">
                {t(column.header)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.map((item) => (
            <tr
              className={onRowClick ? "data-table__interactive" : undefined}
              key={item.id}
              onClick={onRowClick ? (event) => onRowClick(item, event.currentTarget) : undefined}
              onKeyDown={(event) => handleKey(event, item)}
              tabIndex={onRowClick ? 0 : undefined}
            >
              {columns.map((column) => (
                <td className={column.className} key={column.key}>
                  {column.render(item)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function DetailDrawer({
  open,
  onClose,
  eyebrow,
  title,
  children,
  returnFocusRef,
}: {
  open: boolean;
  onClose: () => void;
  eyebrow: string;
  title: string;
  children: ReactNode;
  returnFocusRef?: RefObject<HTMLElement | null>;
}) {
  const { t } = useI18n();
  const titleId = useId();
  const closeRef = useRef<HTMLButtonElement>(null);
  const onCloseRef = useRef(onClose);
  const reduced = useReducedMotion();
  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);
  useEffect(() => {
    if (!open) return;
    const returnTarget = returnFocusRef?.current;
    closeRef.current?.focus();
    const keydown = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") onCloseRef.current();
    };
    document.addEventListener("keydown", keydown);
    return () => {
      document.removeEventListener("keydown", keydown);
      requestAnimationFrame(() => {
        if (returnTarget?.isConnected) returnTarget.focus();
      });
    };
  }, [open, returnFocusRef]);
  return (
    <AnimatePresence>
      {open ? (
        <div className="drawer-layer">
          <motion.button
            aria-label={t("Close detail drawer")}
            className="drawer-backdrop"
            initial={reduced ? false : { opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={onClose}
          />
          <motion.aside
            aria-labelledby={titleId}
            aria-modal="true"
            className="detail-drawer"
            initial={reduced ? false : { x: "100%" }}
            animate={{ x: 0 }}
            exit={{ x: "100%" }}
            role="dialog"
            transition={{ type: "spring", stiffness: 420, damping: 38 }}
          >
            <header className="detail-drawer__header">
              <div>
                <p className="eyebrow">{t(eyebrow)}</p>
                <h2 id={titleId}>{t(title)}</h2>
              </div>
              <Button
                aria-label={t("Close drawer")}
                onClick={onClose}
                ref={closeRef}
                size="sm"
                variant="ghost"
              >
                <X aria-hidden="true" size={18} />
              </Button>
            </header>
            <div className="detail-drawer__body">{children}</div>
          </motion.aside>
        </div>
      ) : null}
    </AnimatePresence>
  );
}

export function Dialog({
  open,
  onClose,
  title,
  description,
  children,
  size = "default",
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  description: string;
  children: ReactNode;
  size?: "default" | "wide";
}) {
  const { t } = useI18n();
  const titleId = useId();
  const descriptionId = useId();
  const reduced = useReducedMotion();
  useEffect(() => {
    if (!open) return;
    const keydown = (event: globalThis.KeyboardEvent) => event.key === "Escape" && onClose();
    document.addEventListener("keydown", keydown);
    return () => document.removeEventListener("keydown", keydown);
  }, [onClose, open]);
  return (
    <AnimatePresence>
      {open ? (
        <div className="dialog-layer">
          <motion.button
            aria-label={t("Close dialog")}
            className="dialog-backdrop"
            onClick={onClose}
          />
          <motion.div
            aria-describedby={descriptionId}
            aria-labelledby={titleId}
            aria-modal="true"
            className={cn("dialog", size === "wide" && "dialog--wide")}
            initial={reduced ? false : { opacity: 0, scale: 0.96, y: 8 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.98 }}
            role="dialog"
          >
            <header>
              <div>
                <h2 id={titleId}>{t(title)}</h2>
                <p id={descriptionId}>{t(description)}</p>
              </div>
              <Button aria-label={t("Close dialog")} onClick={onClose} size="sm" variant="ghost">
                <X size={18} />
              </Button>
            </header>
            <div className="dialog__body">{children}</div>
          </motion.div>
        </div>
      ) : null}
    </AnimatePresence>
  );
}

export function DefinitionList({ items }: { items: Array<{ label: string; value: ReactNode }> }) {
  const { t } = useI18n();
  return (
    <dl className="definition-list">
      {items.map((item) => (
        <div key={item.label}>
          <dt>{t(item.label)}</dt>
          <dd>{typeof item.value === "string" ? t(item.value) : item.value}</dd>
        </div>
      ))}
    </dl>
  );
}

export function TimelineStep({
  label,
  complete,
  description,
  last,
}: {
  label: string;
  complete: boolean;
  description?: string;
  last?: boolean;
}) {
  const { t } = useI18n();
  return (
    <div className="timeline-step">
      <div className="timeline-step__rail">
        <span className={cn("timeline-step__dot", complete && "timeline-step__dot--complete")}>
          {complete ? <Check size={12} /> : <Clock3 size={12} />}
        </span>
        {!last ? <span className="timeline-step__line" /> : null}
      </div>
      <div>
        <strong>{t(label)}</strong>
        {description ? <p>{t(description)}</p> : null}
      </div>
    </div>
  );
}

export function UnavailableNotice({ children }: { children: ReactNode }) {
  const { t } = useI18n();
  return (
    <div className="unavailable-notice">
      <CircleSlash aria-hidden="true" size={17} />
      <span>{localizeChildren(children, t)}</span>
    </div>
  );
}

export function RowChevron() {
  return <ChevronRight aria-hidden="true" className="row-chevron" size={15} />;
}

export function InlineLoading({ label }: { label: string }) {
  const { t } = useI18n();
  return (
    <span className="inline-loading">
      <LoaderCircle aria-hidden="true" size={14} />
      {t(label)}
    </span>
  );
}
