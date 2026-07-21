import { Link } from "@tanstack/react-router";
import { ArrowLeft, Compass } from "lucide-react";

import { EmptyState } from "../components/ui";
import { PageFrame } from "../components/workspace";

export function NotFoundPage() {
  return (
    <PageFrame>
      <div className="not-found">
        <EmptyState
          action={
            <Link className="button button--primary button--md" search={{}} to="/">
              <ArrowLeft size={14} /> Return home
            </Link>
          }
          description="This route is not part of the Connect, Trust, Protect, Audit, or System information architecture."
          title="Workspace not found"
        />
        <Compass aria-hidden="true" size={180} />
      </div>
    </PageFrame>
  );
}
