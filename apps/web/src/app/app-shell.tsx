import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  Bell,
  Cable,
  ChevronLeft,
  ChevronRight,
  CircleHelp,
  Clock3,
  Command,
  Home,
  Menu,
  Settings,
  ShieldCheck,
  UserRoundCheck,
} from "lucide-react";
import { useEffect, useState } from "react";

import { isMockMode, requestOperation } from "../lib/api";
import { synchronizeLiveEvent } from "../lib/query-sync";
import { LiveEventsContext, useLiveEvents } from "../lib/use-live-events";
import type { Scenario } from "../types";
import { CommandPalette } from "../components/command-palette";
import { Button, SourceBadge, StatusOrb, cn } from "../components/ui";

const navItems = [
  { label: "Connect", route: "/connect/$section", section: "overview", icon: Cable },
  { label: "Trust", route: "/trust/$section", section: "agents", icon: UserRoundCheck },
  { label: "Protect", route: "/protect/$section", section: "policies", icon: ShieldCheck },
  { label: "Audit", route: "/audit/$section", section: "analytics", icon: Activity },
] as const;

const scenarios: Array<{ value: Scenario; label: string }> = [
  { value: "normal", label: "Live mock" },
  { value: "empty", label: "No data" },
  { value: "loading", label: "Loading" },
  { value: "partial", label: "Partial failure" },
  { value: "error", label: "Total failure" },
];

export function AppShell() {
  const mocksEnabled = isMockMode();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const location = useRouterState({ select: (state) => state.location });
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem("agentshark.sidebar") === "collapsed",
  );
  const [mobileOpen, setMobileOpen] = useState(false);
  const [commandOpen, setCommandOpen] = useState(false);
  const scenario = (new URLSearchParams(location.searchStr).get("scenario") ??
    "normal") as Scenario;
  const overview = useQuery({
    queryKey: ["overview", scenario],
    queryFn: ({ signal }) => requestOperation("getOverview", signal),
    staleTime: 15_000,
    retry: false,
  });
  const live = useLiveEvents(overview.isSuccess && scenario !== "empty");

  useEffect(() => {
    localStorage.setItem("agentshark.sidebar", collapsed ? "collapsed" : "expanded");
  }, [collapsed]);
  useEffect(() => setMobileOpen(false), [location.pathname]);
  useEffect(() => {
    const event = live.events[0];
    if (event) void synchronizeLiveEvent(queryClient, event);
  }, [live.events[0]?.id, queryClient]);

  const pending =
    overview.data?.data.metrics.find((metric) => metric.id === "approvals")?.value ?? 0;
  const health = overview.data?.data.health ?? [];

  return (
    <div className={cn("app-shell", collapsed && "app-shell--collapsed")}>
      <button
        aria-label="Close navigation"
        className={cn("mobile-scrim", mobileOpen && "mobile-scrim--visible")}
        onClick={() => setMobileOpen(false)}
      />
      <aside className={cn("sidebar", mobileOpen && "sidebar--mobile-open")}>
        <div className="brand">
          <span aria-hidden="true" className="brand__mark">
            <span />
            <span />
            <span />
          </span>
          <span className="brand__copy">
            <strong>Agentshark</strong>
            <small>CONTROL PLANE</small>
          </span>
        </div>
        <div className="environment-card">
          <span className="environment-card__glyph">PX</span>
          <span>
            <small>Environment</small>
            <strong>{overview.data?.data.environment ?? "Connecting"}</strong>
          </span>
          <StatusOrb
            label="Environment health"
            status={
              health.length === 0
                ? "connecting"
                : health.every((item) => item.status === "healthy")
                  ? "healthy"
                  : "degraded"
            }
          />
        </div>
        <nav aria-label="Primary navigation" className="primary-nav">
          <p className="nav-label">Workspaces</p>
          <Link
            aria-current={location.pathname === "/" ? "page" : undefined}
            className={cn("nav-item", location.pathname === "/" && "nav-item--active")}
            search={{ scenario: scenario === "normal" ? undefined : scenario }}
            to="/"
          >
            <Home aria-hidden="true" size={18} />
            <span>Home</span>
          </Link>
          {navItems.map((item) => {
            const Icon = item.icon;
            const active = location.pathname.startsWith(`/${item.label.toLowerCase()}`);
            return (
              <Link
                aria-current={active ? "page" : undefined}
                className={cn("nav-item", active && "nav-item--active")}
                key={item.label}
                params={{ section: item.section }}
                search={{ scenario: scenario === "normal" ? undefined : scenario }}
                to={item.route}
              >
                <Icon aria-hidden="true" size={18} />
                <span>{item.label}</span>
                {item.label === "Protect" && pending > 0 ? <em>{pending}</em> : null}
              </Link>
            );
          })}
        </nav>
        <div className="sidebar__bottom">
          <Link
            className={cn("nav-item", location.pathname === "/system" && "nav-item--active")}
            search={{ scenario: scenario === "normal" ? undefined : scenario }}
            to="/system"
          >
            <Settings size={18} />
            <span>System</span>
          </Link>
          <a
            className="nav-item"
            href="https://github.com/Thespectier/AgentsharkX"
            rel="noreferrer"
            target="_blank"
          >
            <CircleHelp size={18} />
            <span>Documentation</span>
          </a>
          <button
            aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
            className="sidebar-toggle"
            onClick={() => setCollapsed((value) => !value)}
          >
            {collapsed ? (
              <ChevronRight size={16} />
            ) : (
              <>
                <ChevronLeft size={16} />
                <span>Collapse</span>
              </>
            )}
          </button>
        </div>
      </aside>

      <div className="app-frame">
        <header className="topbar">
          <div className="topbar__left">
            <Button
              aria-label="Open navigation"
              className="mobile-menu"
              onClick={() => setMobileOpen(true)}
              size="sm"
              variant="ghost"
            >
              <Menu size={18} />
            </Button>
            <button
              aria-label="Search or jump to commands"
              className="command-trigger"
              onClick={() => setCommandOpen(true)}
            >
              <Command aria-hidden="true" size={15} />
              <span>Search or jump to…</span>
              <kbd>⌘K</kbd>
            </button>
          </div>
          <div className="topbar__right">
            <div className={cn("mock-indicator", !mocksEnabled && "mock-indicator--live")}>
              <span /> {mocksEnabled ? "MOCK DATA" : "LIVE BFF"}
            </div>
            {mocksEnabled ? (
              <label className="scenario-select">
                <span className="sr-only">Demo state</span>
                <select
                  aria-label="Demo state"
                  onChange={(event) => {
                    const next = event.target.value as Scenario;
                    const search = new URLSearchParams(location.searchStr);
                    if (next === "normal") search.delete("scenario");
                    else search.set("scenario", next);
                    search.delete("event");
                    const query = search.toString();
                    void navigate({ href: `${location.pathname}${query ? `?${query}` : ""}` });
                  }}
                  value={scenario}
                >
                  {scenarios.map((item) => (
                    <option key={item.value} value={item.value}>
                      {item.label}
                    </option>
                  ))}
                </select>
              </label>
            ) : null}
            <div className="source-health" aria-label="Upstream health">
              {health.length ? (
                health.map((item) => (
                  <span key={item.source}>
                    <StatusOrb
                      label={`${item.label} ${item.status}`}
                      status={
                        scenario === "partial" && item.source === "agentguard"
                          ? "degraded"
                          : item.status
                      }
                    />
                    <SourceBadge source={item.source} />
                  </span>
                ))
              ) : (
                <span>
                  <StatusOrb status={overview.isError ? "down" : "connecting"} />
                  Sources
                </span>
              )}
            </div>
            <div aria-label="Data window: last 60 minutes" className="time-range">
              <Clock3 size={15} />
              <span>Last 60m</span>
            </div>
            <Link
              aria-label={`${pending} pending approvals`}
              className="topbar-icon"
              params={{ section: "approvals" }}
              search={{ scenario: scenario === "normal" ? undefined : scenario }}
              to="/protect/$section"
            >
              <Bell size={17} />
              {pending ? <span>{pending}</span> : null}
            </Link>
            <Link
              aria-label="Open system settings"
              className="avatar"
              search={{ scenario: scenario === "normal" ? undefined : scenario }}
              to="/system"
            >
              AS
            </Link>
          </div>
        </header>
        <main className="main-content" id="main-content" tabIndex={-1}>
          <LiveEventsContext.Provider value={live}>
            <Outlet />
          </LiveEventsContext.Provider>
        </main>
      </div>
      <CommandPalette onOpenChange={setCommandOpen} open={commandOpen} />
    </div>
  );
}
