import {
  Activity,
  CheckCircle2,
  CircleSlash,
  Database,
  ExternalLink,
  ServerCog,
  ShieldCheck,
} from "lucide-react";

import { PageFrame } from "../components/workspace";
import {
  Card,
  CardHeader,
  PageHeader,
  SourceBadge,
  StatusBadge,
  StatusOrb,
} from "../components/ui";

const capabilities = [
  {
    id: "gateway.runtime",
    source: "agentgateway" as const,
    status: "supported",
    label: "Runtime & config discovery",
  },
  {
    id: "gateway.logs",
    source: "agentgateway" as const,
    status: "partial",
    label: "Request logs (database required)",
  },
  {
    id: "gateway.adminAuth",
    source: "agentgateway" as const,
    status: "unavailable",
    label: "Native admin authentication",
  },
  {
    id: "guard.resources",
    source: "agentguard" as const,
    status: "supported",
    label: "Tools, Skills & MCP resources",
  },
  {
    id: "guard.agents",
    source: "agentguard" as const,
    status: "partial",
    label: "Agent list derived from explicit resource/session IDs",
  },
  {
    id: "guard.approvals",
    source: "agentguard" as const,
    status: "supported",
    label: "Approval queue and decisions",
  },
];

export function SystemPage() {
  return (
    <PageFrame>
      <PageHeader
        description="Diagnostics support the four product workspaces; System is not a fifth capability layer."
        eyebrow="System / Diagnostics"
        title="Sources, versions & capabilities"
      />
      <div className="source-card-grid">
        <Card elevated>
          <CardHeader
            action={<StatusBadge status="healthy" />}
            description="Standalone management plane"
            title="agentgateway"
          />
          <div className="system-source">
            <span className="system-source__icon">
              <ServerCog size={24} />
            </span>
            <div>
              <SourceBadge source="agentgateway" />
              <strong>v1.3.1</strong>
              <span>dbaaf7ed · 18 ms</span>
            </div>
          </div>
          <ul className="diagnostic-list">
            <li>
              <CheckCircle2 size={14} /> Readiness <code>/healthz/ready</code>
            </li>
            <li>
              <CheckCircle2 size={14} /> Runtime/config probes
            </li>
            <li>
              <CircleSlash size={14} /> Request-log database not configured in Phase 0
            </li>
          </ul>
          <a
            className="text-link"
            href="http://localhost:15000/ui"
            rel="noreferrer"
            target="_blank"
          >
            Open native console <ExternalLink size={13} />
          </a>
        </Card>
        <Card elevated>
          <CardHeader
            action={<StatusBadge status="healthy" />}
            description="Runtime security control plane"
            title="AgentGuard"
          />
          <div className="system-source">
            <span className="system-source__icon system-source__icon--guard">
              <ShieldCheck size={24} />
            </span>
            <div>
              <SourceBadge source="agentguard" />
              <strong>v2.1 · API 0.3.0</strong>
              <span>6f95deb9 · 24 ms</span>
            </div>
          </div>
          <ul className="diagnostic-list">
            <li>
              <CheckCircle2 size={14} /> Protected management health
            </li>
            <li>
              <CheckCircle2 size={14} /> 45 OpenAPI routes detected
            </li>
            <li>
              <Activity size={14} /> 31 explicit agent IDs observed
            </li>
          </ul>
          <a className="text-link" href="http://localhost:38008" rel="noreferrer" target="_blank">
            Open native console <ExternalLink size={13} />
          </a>
        </Card>
      </div>
      <Card>
        <CardHeader
          action={
            <span className="fetched-at">
              <Database size={13} /> Probe based
            </span>
          }
          description="The UI hides, disables, or links out according to this registry. Version numbers alone never enable a feature."
          title="Capability registry"
        />
        <div className="capability-list">
          {capabilities.map((capability) => (
            <div key={capability.id}>
              <StatusOrb
                status={
                  capability.status === "supported"
                    ? "healthy"
                    : capability.status === "partial"
                      ? "degraded"
                      : "down"
                }
              />
              <div>
                <strong>{capability.label}</strong>
                <code>{capability.id}</code>
              </div>
              <SourceBadge source={capability.source} />
              <StatusBadge status={capability.status} />
            </div>
          ))}
        </div>
      </Card>
    </PageFrame>
  );
}
