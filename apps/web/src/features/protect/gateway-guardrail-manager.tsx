import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Braces,
  CheckCircle2,
  LoaderCircle,
  Pencil,
  Plus,
  ShieldCheck,
  Trash2,
} from "lucide-react";
import { type KeyboardEvent, useState } from "react";

import {
  Button,
  Card,
  CardHeader,
  DefinitionList,
  Dialog,
  EmptyState,
  ErrorState,
  SourceBadge,
  StatusBadge,
} from "../../components/ui";
import type {
  GatewayPolicyMutationReceipt,
  GatewayPolicySetting,
} from "../../generated/api-client";
import {
  ApiError,
  formatError,
  getScenario,
  mutateOperation,
  requestOperation,
} from "../../lib/api";
import { formatTimeWithZone } from "../../lib/format";
import { useI18n } from "../../lib/i18n";
import {
  arrayValue,
  guardKind,
  isRecord,
  jsonText,
  objectValue,
  targetKind,
  type GuardrailFamily,
  type JsonRecord,
} from "./gateway-guardrail-model";
import { GatewayGuardrailEditor } from "./gateway-guardrail-editor";

type GuardrailView = "llm" | "mcp";
type GuardrailMutationTarget = {
  item: GatewayPolicySetting;
  revisionToken: string;
};

const guardrailPaths: Record<GuardrailView, string> = {
  llm: "/llm/policies/guardrails",
  mcp: "/mcp/policies/mcpGuardrails",
};

export function GatewayGuardrailManager() {
  const { t } = useI18n();
  const scenario = getScenario();
  const queryClient = useQueryClient();
  const [view, setView] = useState<GuardrailView>("llm");
  const [editor, setEditor] = useState<GuardrailMutationTarget>();
  const [deleteTarget, setDeleteTarget] = useState<GuardrailMutationTarget>();
  const [deleteConfirmed, setDeleteConfirmed] = useState(false);
  const [receipt, setReceipt] = useState<GatewayPolicyMutationReceipt>();
  const queryKey = ["protect-gateway-policy-configuration", scenario] as const;
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => requestOperation("getGatewayPolicyConfiguration", signal),
    retry: false,
  });

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey });
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["protect"] }),
      queryClient.invalidateQueries({ queryKey: ["connect-summary"] }),
      queryClient.invalidateQueries({ queryKey: ["connect-llm-configuration"] }),
      queryClient.invalidateQueries({ queryKey: ["connect-mcp-configuration"] }),
      queryClient.invalidateQueries({ queryKey: ["connect-models"] }),
      queryClient.invalidateQueries({ queryKey: ["connect-mcp"] }),
    ]);
  };

  const save = useMutation({
    mutationFn: async ({
      target,
      value,
    }: {
      target: GuardrailMutationTarget;
      value: JsonRecord;
    }) => {
      return mutateOperation(
        "upsertGatewayPolicy",
        { revisionToken: target.revisionToken, value },
        { path: { resourceId: target.item.id } },
      );
    },
    onSuccess: async (response) => {
      setReceipt(response.data);
      setEditor(undefined);
      await refresh();
    },
    onError: async () => queryClient.invalidateQueries({ queryKey }),
  });

  const remove = useMutation({
    mutationFn: async (target: GuardrailMutationTarget) => {
      return mutateOperation(
        "deleteGatewayPolicy",
        { revisionToken: target.revisionToken, confirmed: true },
        { path: { resourceId: target.item.id } },
      );
    },
    onSuccess: async (response) => {
      setReceipt(response.data);
      setDeleteTarget(undefined);
      setDeleteConfirmed(false);
      await refresh();
    },
    onError: async () => queryClient.invalidateQueries({ queryKey }),
  });

  if (query.isLoading) {
    return <div className="resource-note">{t("Loading gateway guardrails...")}</div>;
  }
  if (query.isError || !query.data) {
    return (
      <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
    );
  }

  const settings = query.data.data.settings;
  const revisionToken = query.data.data.revisionToken;
  const selected = settings.find(
    (item) => item.family === view && item.rawRef.id === guardrailPaths[view],
  );

  return (
    <div className="stack gateway-guardrail-manager">
      {receipt ? <MutationReceipt receipt={receipt} /> : null}
      <div className="gateway-guardrail-toolbar">
        <div aria-label={t("Gateway guardrail family")} className="mcp-segmented" role="tablist">
          {(["llm", "mcp"] as const).map((family) => (
            <button
              aria-controls="gateway-guardrail-panel"
              aria-selected={view === family}
              className={view === family ? "is-active" : undefined}
              id={`gateway-guardrail-tab-${family}`}
              key={family}
              onClick={() => setView(family)}
              onKeyDown={(event) => handleTabKey(event, view, setView)}
              role="tab"
              tabIndex={view === family ? 0 : -1}
              type="button"
            >
              <ShieldCheck aria-hidden="true" size={15} /> {family.toUpperCase()}
            </button>
          ))}
        </div>
        <div className="gateway-guardrail-toolbar__context">
          <SourceBadge source="agentgateway" />
          <span>{t("Complete source-owned configuration")}</span>
        </div>
      </div>

      <div
        aria-labelledby={`gateway-guardrail-tab-${view}`}
        id="gateway-guardrail-panel"
        role="tabpanel"
      >
        {selected ? (
          <GuardrailWorkspace
            item={selected}
            onDelete={() => {
              remove.reset();
              setDeleteConfirmed(false);
              setDeleteTarget({ item: selected, revisionToken });
            }}
            onEdit={() => {
              save.reset();
              setEditor({ item: selected, revisionToken });
            }}
          />
        ) : (
          <EmptyState
            description="The connection service did not report the verified global guardrail path for this family."
            title="Guardrail configuration unavailable"
          />
        )}
      </div>

      <Dialog
        description="Edit the complete source-owned guardrail object. Structured controls preserve advanced JSON fields."
        onClose={() => !save.isPending && setEditor(undefined)}
        open={Boolean(editor)}
        size="wide"
        title={
          editor
            ? `${t(editor.item.enabled ? "Edit" : "Configure")} ${t(editor.item.title)}`
            : "Edit guardrails"
        }
      >
        {editor ? (
          <GatewayGuardrailEditor
            error={save.isError ? save.error : undefined}
            family={editor.item.family as GuardrailFamily}
            initial={
              editor.item.enabled && isRecord(editor.item.value) ? editor.item.value : undefined
            }
            key={`${editor.item.id}:${editor.item.enabled}:${editor.revisionToken}`}
            onCancel={() => setEditor(undefined)}
            onSave={(value) => save.mutate({ target: editor, value })}
            pending={save.isPending}
          />
        ) : null}
      </Dialog>

      <DeleteGuardrailDialog
        confirmed={deleteConfirmed}
        error={remove.isError ? remove.error : undefined}
        item={deleteTarget?.item}
        onClose={() => !remove.isPending && setDeleteTarget(undefined)}
        onConfirm={() => deleteTarget && remove.mutate(deleteTarget)}
        onToggle={setDeleteConfirmed}
        pending={remove.isPending}
      />
    </div>
  );
}

function GuardrailWorkspace({
  item,
  onEdit,
  onDelete,
}: {
  item: GatewayPolicySetting;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { t } = useI18n();
  const family = item.family as GuardrailFamily;
  return (
    <Card className="gateway-guardrail-workspace">
      <CardHeader
        action={
          <div className="gateway-guardrail-actions">
            <Button onClick={onEdit} size="sm" variant="primary">
              {item.enabled ? (
                <Pencil aria-hidden="true" size={14} />
              ) : (
                <Plus aria-hidden="true" size={14} />
              )}
              {item.enabled ? "Edit guardrails" : "Configure guardrails"}
            </Button>
            <Button
              aria-label={`${t("Delete guardrails")}: ${item.title}`}
              disabled={!item.enabled}
              onClick={onDelete}
              size="sm"
              title={`${t("Delete guardrails")}: ${item.title}`}
              variant="danger"
            >
              <Trash2 aria-hidden="true" size={14} />
            </Button>
          </div>
        }
        description={
          family === "llm"
            ? "Ordered prompt and response guards applied to every configured LLM model."
            : "Ordered remote processors applied to matching MCP methods."
        }
        title={family === "llm" ? "LLM guardrails" : "MCP guardrails"}
      />
      <DefinitionList
        items={[
          { label: "Source", value: <SourceBadge source={item.source} /> },
          {
            label: "Status",
            value: <StatusBadge status={item.enabled ? "Enabled" : "Disabled"} />,
          },
          { label: "Scope", value: item.scope },
          { label: "Phase", value: item.phase },
          { label: "Action", value: item.action },
          { label: "Raw reference", value: <code>{item.rawRef.id}</code> },
        ]}
      />
      {!item.enabled || !isRecord(item.value) ? (
        <EmptyState
          action={
            <Button onClick={onEdit} variant="primary">
              <Plus aria-hidden="true" size={14} /> Configure guardrails
            </Button>
          }
          compact
          description="No guardrail object is configured at this verified connection path."
          title="Guardrails are disabled"
        />
      ) : family === "llm" ? (
        <LlmGuardrailSummary value={item.value} />
      ) : (
        <McpGuardrailSummary value={item.value} />
      )}
      {item.enabled ? (
        <details className="gateway-guardrail-complete-value">
          <summary>
            <Braces aria-hidden="true" size={14} /> {t("Complete guardrail JSON")}
          </summary>
          <pre>{jsonText(item.value)}</pre>
        </details>
      ) : null}
    </Card>
  );
}

function LlmGuardrailSummary({ value }: { value: JsonRecord }) {
  const { t } = useI18n();
  const request = arrayValue(value.request);
  const response = arrayValue(value.response);
  return (
    <div className="gateway-guardrail-summary-grid">
      <section className="gateway-guardrail-phase">
        <header>
          <div>
            <span>{t("Request guards")}</span>
            <strong>{request.length}</strong>
          </div>
          <StatusBadge
            status={value.streaming === "Enabled" ? "Streaming enabled" : "Streaming disabled"}
          />
        </header>
        <GuardSummaryList guards={request} phase="Request" />
      </section>
      <section className="gateway-guardrail-phase">
        <header>
          <div>
            <span>{t("Response guards")}</span>
            <strong>{response.length}</strong>
          </div>
          <StatusBadge status="Response" />
        </header>
        <GuardSummaryList guards={response} phase="Response" />
      </section>
    </div>
  );
}

function GuardSummaryList({ guards, phase }: { guards: unknown[]; phase: string }) {
  const { t } = useI18n();
  if (!guards.length)
    return <p className="gateway-guardrail-empty-inline">{t("No guards configured")}</p>;
  return (
    <ol className="gateway-guardrail-summary-list">
      {guards.map((guard, index) => (
        <li key={index}>
          <span>{index + 1}</span>
          <div>
            <strong>{t(guardKindLabel(guardKind(guard)))}</strong>
            <small>
              {t(phase)} · {guardDetail(guard, t)}
            </small>
          </div>
        </li>
      ))}
    </ol>
  );
}

function McpGuardrailSummary({ value }: { value: JsonRecord }) {
  const { t } = useI18n();
  const processors = arrayValue(value.processors);
  return (
    <div className="gateway-guardrail-processors">
      <header>
        <span>{t("Ordered processors")}</span>
        <strong>{processors.length}</strong>
      </header>
      {processors.length ? (
        <ol className="gateway-guardrail-summary-list gateway-guardrail-summary-list--processors">
          {processors.map((candidate, index) => {
            const processor = objectValue(candidate);
            const methods = objectValue(processor.methods);
            return (
              <li key={index}>
                <span>{index + 1}</span>
                <div>
                  <strong>{processorTarget(processor)}</strong>
                  <small>
                    {t(String(processor.failureMode ?? "failClosed"))} ·{" "}
                    {Object.keys(methods).length} {t("method matches")}
                  </small>
                </div>
              </li>
            );
          })}
        </ol>
      ) : (
        <p className="gateway-guardrail-empty-inline">{t("No processors configured")}</p>
      )}
    </div>
  );
}

function DeleteGuardrailDialog({
  item,
  confirmed,
  pending,
  error,
  onClose,
  onToggle,
  onConfirm,
}: {
  item?: GatewayPolicySetting;
  confirmed: boolean;
  pending: boolean;
  error?: unknown;
  onClose: () => void;
  onToggle: (confirmed: boolean) => void;
  onConfirm: () => void;
}) {
  const { t } = useI18n();
  return (
    <Dialog
      description="This removes the complete guardrail object from its exact connection configuration path."
      onClose={onClose}
      open={Boolean(item)}
      title="Delete gateway guardrails"
    >
      {item ? (
        <div className="dialog-form gateway-guardrail-delete">
          <DefinitionList
            items={[
              { label: "Guardrails", value: item.title },
              { label: "Source", value: <SourceBadge source={item.source} /> },
              { label: "Raw reference", value: <code>{item.rawRef.id}</code> },
            ]}
          />
          <label className="confirm-field">
            <input
              checked={confirmed}
              onChange={(event) => onToggle(event.target.checked)}
              type="checkbox"
            />
            {t("I confirm this complete connection guardrail configuration should be removed.")}
          </label>
          {error ? <MutationError error={error} /> : null}
          <footer>
            <Button disabled={pending} onClick={onClose} type="button" variant="ghost">
              Cancel
            </Button>
            <Button
              disabled={pending || !confirmed}
              onClick={onConfirm}
              type="button"
              variant="danger"
            >
              {pending ? (
                <LoaderCircle aria-hidden="true" className="spin" size={14} />
              ) : (
                <Trash2 aria-hidden="true" size={14} />
              )}
              Delete guardrails
            </Button>
          </footer>
        </div>
      ) : null}
    </Dialog>
  );
}

function MutationReceipt({ receipt }: { receipt: GatewayPolicyMutationReceipt }) {
  const { t } = useI18n();
  return (
    <div aria-live="polite" className="mutation-receipt" role="status">
      <CheckCircle2 aria-hidden="true" size={17} />
      <div>
        <strong>{t(receipt.message)}</strong>
        <span>
          {receipt.target} · {formatTimeWithZone(receipt.completedAt)} · {t("request")}{" "}
          <code>{receipt.requestId}</code>
        </span>
      </div>
    </div>
  );
}

export function MutationError({ error }: { error: unknown }) {
  const requestId = error instanceof ApiError ? error.failure?.requestId : undefined;
  return (
    <div className="protect-error" role="alert">
      <span>
        {formatError(error)}
        {requestId ? (
          <>
            {" "}
            · Request ID <code>{requestId}</code>
          </>
        ) : null}
      </span>
    </div>
  );
}

function handleTabKey(
  event: KeyboardEvent<HTMLButtonElement>,
  current: GuardrailView,
  select: (view: GuardrailView) => void,
) {
  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  const next =
    event.key === "Home" ? "llm" : event.key === "End" ? "mcp" : current === "llm" ? "mcp" : "llm";
  select(next);
  requestAnimationFrame(() => document.getElementById(`gateway-guardrail-tab-${next}`)?.focus());
}

function guardKindLabel(kind: ReturnType<typeof guardKind>) {
  const labels: Record<ReturnType<typeof guardKind>, string> = {
    regex: "Regex rules",
    webhook: "Webhook",
    openAIModeration: "OpenAI Moderation",
    bedrockGuardrails: "Bedrock Guardrails",
    googleModelArmor: "Google Model Armor",
    azureContentSafety: "Azure Content Safety",
    unsupported: "Complete JSON guard",
  };
  return labels[kind];
}

function guardDetail(value: unknown, translate: (value: string) => string) {
  const guard = objectValue(value);
  const kind = guardKind(guard);
  const body = kind === "unsupported" ? {} : objectValue(guard[kind]);
  if (kind === "regex") return `${arrayValue(body.rules).length} ${translate("rules")}`;
  if (kind === "webhook") return processorTarget(objectValue(body.target));
  if (kind === "openAIModeration") return String(body.model ?? "omni-moderation-latest");
  if (kind === "bedrockGuardrails")
    return String(body.guardrailIdentifier ?? translate("Not configured"));
  if (kind === "googleModelArmor") return String(body.templateId ?? translate("Not configured"));
  if (kind === "azureContentSafety") return String(body.endpoint ?? translate("Not configured"));
  return translate("Preserved source object");
}

function processorTarget(value: JsonRecord) {
  const kind = targetKind(value);
  if (kind === "service") {
    const service = objectValue(value.service);
    return `${String(service.name ?? "service")}:${String(service.port ?? "")}`;
  }
  return String(value[kind] ?? "Target not configured");
}
