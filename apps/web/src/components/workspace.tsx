import { useRouterState } from "@tanstack/react-router";
import { type ReactNode, useEffect } from "react";

export function PageFrame({ children }: { children: ReactNode }) {
  return <div className="page-frame">{children}</div>;
}

export function useWorkspaceSection(area: string, fallback: string): string {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const segments = pathname.split("/").filter(Boolean);
  return segments[0] === area && segments[1] ? segments[1] : fallback;
}

export function useDocumentTitle(title: string) {
  useEffect(() => {
    document.title = `${title} | Agentshark`;
  }, [title]);
}
