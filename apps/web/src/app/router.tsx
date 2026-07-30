import { createRootRoute, createRoute, createRouter, redirect } from "@tanstack/react-router";

import { AppShell } from "./app-shell";
import { AuditPage } from "../features/audit/audit-page";
import { TraceDetailPage, TraceListPage } from "../features/audit/trace-pages";
import { ConnectPage } from "../features/connect/connect-page";
import { DemoLabPage } from "../features/demo/demo-lab-page";
import { HomePage } from "../features/home/home-page";
import { ProtectPage } from "../features/protect/protect-page";
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
  beforeLoad: ({ search }) => {
    throw redirect({ to: "/connect/$section", params: { section: "overview" }, search });
  },
});
const connectSectionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "connect/$section",
  component: ConnectPage,
  beforeLoad: ({ params, search }) => {
    if (["overview", "agents", "llm", "mcp", "traffic"].includes(params.section)) return;
    throw redirect({ to: "/connect/$section", params: { section: "overview" }, search });
  },
});
const trustRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "trust",
  beforeLoad: ({ search }) => {
    throw redirect({ to: "/trust/$section", params: { section: "overview" }, search });
  },
});
const trustSectionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "trust/$section",
  component: TrustPage,
  beforeLoad: ({ params, search }) => {
    if (["overview", "guardrails", "runtime-rules", "policy"].includes(params.section)) return;
    if (params.section === "policies") {
      throw redirect({ to: "/trust/$section", params: { section: "policy" }, search });
    }
    throw redirect({ to: "/trust/$section", params: { section: "overview" }, search });
  },
});
const protectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "protect",
  beforeLoad: ({ search }) => {
    throw redirect({ to: "/protect/$section", params: { section: "overview" }, search });
  },
});
const protectSectionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "protect/$section",
  component: ProtectPage,
  beforeLoad: ({ params, search }) => {
    if (["overview", "approvals"].includes(params.section)) return;
    if (params.section === "guardrails" || params.section === "runtime-rules") {
      throw redirect({ to: "/trust/$section", params: { section: params.section }, search });
    }
    if (params.section === "policies") {
      throw redirect({ to: "/trust/$section", params: { section: "policy" }, search });
    }
    throw redirect({ to: "/protect/$section", params: { section: "overview" }, search });
  },
});
const auditRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "audit",
  beforeLoad: ({ search }) => {
    throw redirect({ to: "/audit/traces", search });
  },
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
  beforeLoad: ({ params, search }) => {
    if (["traffic", "security-events"].includes(params.section)) return;
    throw redirect({ to: "/audit/traces", search });
  },
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
