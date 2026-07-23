import { Link, useRouterState } from "@tanstack/react-router";
import type { ReactNode } from "react";

import { cn } from "./ui";

export function WorkspaceTabs({
  area,
  items,
}: {
  area: "connect" | "trust" | "protect" | "audit";
  items: Array<{ id: string; label: string; badge?: number }>;
}) {
  const location = useRouterState({ select: (state) => state.location });
  const current = location.pathname.split("/")[2] || items[0]?.id;
  return (
    <nav aria-label={`${area} sections`} className="workspace-tabs">
      {items.map((item) => (
        <WorkspaceTab area={area} current={current} item={item} key={item.id} />
      ))}
    </nav>
  );
}

function WorkspaceTab({
  area,
  current,
  item,
}: {
  area: "connect" | "trust" | "protect" | "audit";
  current?: string;
  item: { id: string; label: string; badge?: number };
}) {
  const content = (
    <>
      {item.label}
      {item.badge ? <span>{item.badge}</span> : null}
    </>
  );
  const shared = {
    "aria-current": current === item.id ? ("page" as const) : undefined,
    className: cn("workspace-tab", current === item.id && "workspace-tab--active"),
    params: { section: item.id },
    search: true as const,
  };
  if (area === "connect")
    return (
      <Link {...shared} to="/connect/$section">
        {content}
      </Link>
    );
  if (area === "trust")
    return (
      <Link {...shared} to="/trust/$section">
        {content}
      </Link>
    );
  if (area === "protect")
    return (
      <Link {...shared} to="/protect/$section">
        {content}
      </Link>
    );
  return (
    <Link {...shared} to="/audit/$section">
      {content}
    </Link>
  );
}

export function PageFrame({ children }: { children: ReactNode }) {
  return <div className="page-frame">{children}</div>;
}

export function useWorkspaceSection(area: string, fallback: string): string {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const segments = pathname.split("/").filter(Boolean);
  return segments[0] === area && segments[1] ? segments[1] : fallback;
}
