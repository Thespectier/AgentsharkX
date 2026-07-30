import { AnimatePresence, motion, useReducedMotion } from "motion/react";
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

import { SeverityBadge, SourceBadge, cn } from "../components/ui";
import {
  displayTimeZoneLabel,
  formatCount,
  formatTimeWithZone,
  formatTrendTick,
  formatTrendTimestamp,
  productizeText,
} from "../lib/format";
import { useI18n } from "../lib/i18n";
import type { TrendPoint, UnifiedEvent } from "../types";

export function ActivityRail({ events, limit = 6 }: { events: UnifiedEvent[]; limit?: number }) {
  const reduced = useReducedMotion();
  const { t } = useI18n();
  return (
    <div className="activity-rail">
      <AnimatePresence initial={false}>
        {events.slice(0, limit).map((event, index) => (
          <motion.article
            animate={{ opacity: 1, y: 0 }}
            className={cn("activity-item", index === 0 && "activity-item--new")}
            exit={{ opacity: 0, height: 0 }}
            initial={reduced ? false : { opacity: 0, y: -10 }}
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
              <p>{productizeText(event.summary)}</p>
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

export function RequestTrendChart({ data }: { data: TrendPoint[] }) {
  const reduced = useReducedMotion();
  const { locale, t } = useI18n();

  if (data.length === 0) {
    return (
      <div className="trend-empty" role="status">
        {t("No traffic or decision data in the last 60 minutes.")}
      </div>
    );
  }

  return (
    <div
      aria-label={t(
        "{mode} trend chart for the last 60 minutes in {count} five-minute Beijing-time buckets",
        { mode: t("requests"), count: data.length },
      )}
      className="chart-wrap"
      role="img"
    >
      <ResponsiveContainer height="100%" width="100%">
        <AreaChart data={data} margin={{ left: 0, right: 0, top: 12, bottom: 0 }}>
          <defs>
            <linearGradient id="chart-requests" x1="0" x2="0" y1="0" y2="1">
              <stop offset="0%" stopColor="#5c92ff" stopOpacity={0.28} />
              <stop offset="100%" stopColor="#5c92ff" stopOpacity={0} />
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
            tickFormatter={formatCount}
            tickLine={false}
            width={45}
            yAxisId="traffic"
          />
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
          <Tooltip
            contentStyle={{
              background: "#101927",
              border: "1px solid #26384d",
              borderRadius: 8,
              color: "#eef5ff",
              fontSize: 12,
            }}
            cursor={{ stroke: "#41536a" }}
            formatter={(value, name) => [formatCount(Number(value)), String(name)]}
            labelFormatter={(value) =>
              `${formatTrendTimestamp(String(value), locale)} ${displayTimeZoneLabel} · ${t("5-minute bucket")}`
            }
          />
          <Area
            animationDuration={reduced ? 0 : 650}
            dataKey="requests"
            fill="url(#chart-requests)"
            name={t("Requests")}
            stroke="#5c92ff"
            strokeWidth={2}
            type="monotone"
            yAxisId="traffic"
          />
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
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
