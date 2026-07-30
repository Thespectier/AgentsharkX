import { createRootRoute, createRoute, createRouter } from "@tanstack/react-router";

import { AppShell } from "./app-shell";
import { AuditPage } from "../features/audit/audit-page";
import { TraceDetailPage, TraceListPage } from "../features/audit/trace-pages";
import { ConnectPage } from "../features/connect/connect-page";
import { DemoLabPage } from "../features/demo/demo-lab-page";
import { HomePage } from "../features/home/home-page";
import { ProtectPage } from "../features/protect/protect-page";
import { SystemPage } from "../features/system-page";
import { TrustPage } from "../features/trust/trust-page";
import { NotFoundPage } from "../features/not-found-page";
import type { Scenario } from "../types";

export type RootSearch = {
  scenario?: Scenario;
  event?: string;
  sessionId?: string;
  ticketId?: string;
};

export function parseSearch(search: Record<string, unknown>): RootSearch {
  const scenario = ["empty", "loading", "partial", "error"].includes(String(search.scenario))
    ? (search.scenario as Scenario)
    : undefined;
  return {
    scenario,
    event: typeof search.event === "string" ? search.event : undefined,
    sessionId: typeof search.sessionId === "string" ? search.sessionId : undefined,
    ticketId: typeof search.ticketId === "string" ? search.ticketId : undefined,
  };
}

const rootRoute = createRootRoute({
  component: AppShell,
  notFoundComponent: NotFoundPage,
  validateSearch: parseSearch,
});

const homeRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: HomePage });
const connectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "connect",
  component: ConnectPage,
});
const connectSectionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "connect/$section",
  component: ConnectPage,
});
const trustRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "trust",
  component: TrustPage,
});
const trustSectionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "trust/$section",
  component: TrustPage,
});
const protectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "protect",
  component: ProtectPage,
});
const protectSectionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "protect/$section",
  component: ProtectPage,
});
const auditRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "audit",
  component: TraceListPage,
});
const auditTracesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "audit/traces",
  component: TraceListPage,
});
const auditTraceDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "audit/traces/$traceId",
  component: TraceDetailPage,
});
const auditSectionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "audit/$section",
  component: AuditPage,
});
const systemRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "system",
  component: SystemPage,
});
const demoRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "demo",
  component: DemoLabPage,
});

const routeTree = rootRoute.addChildren([
  homeRoute,
  connectRoute,
  connectSectionRoute,
  trustRoute,
  trustSectionRoute,
  protectRoute,
  protectSectionRoute,
  auditRoute,
  auditTracesRoute,
  auditTraceDetailRoute,
  auditSectionRoute,
  demoRoute,
  systemRoute,
]);

export const router = createRouter({
  routeTree,
  defaultPreload: "intent",
  defaultPreloadStaleTime: 30_000,
  scrollRestoration: true,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
