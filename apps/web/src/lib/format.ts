import type { Metric, Source } from "../types";

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
    timeZone: "UTC",
  }).format(date);
}

export function formatTrendTimestamp(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
    timeZone: "UTC",
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
    timeZone: "UTC",
  }).format(date);
}

export function sourceLabel(source: Source): string {
  return source === "agentgateway" ? "agentgateway" : "AgentGuard";
}
