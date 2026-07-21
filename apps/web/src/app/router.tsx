import { createRootRoute, createRoute, createRouter } from "@tanstack/react-router";

import { AppShell } from "./app-shell";
import { AuditPage } from "../features/audit/audit-page";
import { ConnectPage } from "../features/connect/connect-page";
import { HomePage } from "../features/home/home-page";
import { ProtectPage } from "../features/protect/protect-page";
import { SystemPage } from "../features/system-page";
import { TrustPage } from "../features/trust/trust-page";
import { NotFoundPage } from "../features/not-found-page";
import type { Scenario } from "../types";

type RootSearch = { scenario?: Scenario; event?: string };

function parseSearch(search: Record<string, unknown>): RootSearch {
  const scenario = ["empty", "loading", "partial", "error"].includes(String(search.scenario))
    ? (search.scenario as Scenario)
    : undefined;
  return {
    scenario,
    event: typeof search.event === "string" ? search.event : undefined,
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
  component: AuditPage,
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

const routeTree = rootRoute.addChildren([
  homeRoute,
  connectRoute,
  connectSectionRoute,
  trustRoute,
  trustSectionRoute,
  protectRoute,
  protectSectionRoute,
  auditRoute,
  auditSectionRoute,
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
