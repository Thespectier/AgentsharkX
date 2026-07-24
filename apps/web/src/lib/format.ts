import type { Metric, Source } from "../types";
import type { Locale } from "./i18n";

export const displayTimeZone = "Asia/Shanghai";
export const displayTimeZoneLabel = "UTC+8";

const integer = new Intl.NumberFormat("en-US", { maximumFractionDigits: 0 });
const decimal = new Intl.NumberFormat("en-US", { maximumFractionDigits: 1 });
const currency = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  maximumFractionDigits: 2,
});

export function formatMetric(metric: Pick<Metric, "value" | "format">): string {
  switch (metric.format) {
    case "currency":
      return currency.format(metric.value);
    case "duration":
      return `${integer.format(metric.value)} ms`;
    case "percent":
      return `${metric.value.toFixed(1)}%`;
    default:
      return integer.format(metric.value);
  }
}

export function formatCount(value: number): string {
  return integer.format(value);
}

export function formatDuration(value: number): string {
  return `${decimal.format(value)} ms`;
}

export function formatTrendTick(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("en-US", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
    timeZone: displayTimeZone,
  }).format(date);
}

export function formatTrendTimestamp(value: string, locale: Locale = "en"): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(locale === "zh-CN" ? "zh-CN" : "en-GB", {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
    timeZone: displayTimeZone,
  }).format(date);
}

export function formatTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("en-US", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
    timeZone: displayTimeZone,
  }).format(date);
}

export function formatTimeWithZone(value: string): string {
  const formatted = formatTime(value);
  return formatted === value ? value : `${formatted} ${displayTimeZoneLabel}`;
}

export function sourceLabel(source: Source): string {
  return source === "agentgateway" ? "agentgateway" : "AgentGuard";
}
