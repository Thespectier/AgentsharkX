import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Bot,
  Boxes,
  Braces,
  CheckCircle2,
  LoaderCircle,
  Pencil,
  Plus,
  Save,
  ShieldCheck,
  Trash2,
} from "lucide-react";
import { type FormEvent, type KeyboardEvent, type ReactNode, useMemo, useState } from "react";

import {
  Button,
  Card,
  CardHeader,
  DataTable,
  DefinitionList,
  Dialog,
  EmptyState,
  ErrorState,
  SourceBadge,
  StatusBadge,
  type Column,
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

type PolicyView = "llm-model" | "mcp";
type SaveTarget = { item: GatewayPolicySetting; value: unknown };

export function GatewayPolicyManager() {
  const { t } = useI18n();
  const scenario = getScenario();
  const queryClient = useQueryClient();
  const [view, setView] = useState<PolicyView>("llm-model");
  const [search, setSearch] = useState("");
  const [editor, setEditor] = useState<GatewayPolicySetting>();
  const [deleteTarget, setDeleteTarget] = useState<GatewayPolicySetting>();
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
    mutationFn: async ({ item, value }: SaveTarget) => {
      const revisionToken = query.data?.data.revisionToken;
      if (!revisionToken) {
        throw new Error(t("The configuration revision is unavailable. Refresh and retry."));
      }
      return mutateOperation(
        "upsertGatewayPolicy",
        { revisionToken, value },
        { path: { resourceId: item.id } },
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
    mutationFn: async (item: GatewayPolicySetting) => {
      const revisionToken = query.data?.data.revisionToken;
      if (!revisionToken) {
        throw new Error(t("The configuration revision is unavailable. Refresh and retry."));
      }
      return mutateOperation(
        "deleteGatewayPolicy",
        { revisionToken, confirmed: true },
        { path: { resourceId: item.id } },
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
    return <div className="resource-note">{t("Loading gateway policy configuration...")}</div>;
  }
  if (query.isError || !query.data) {
    return (
      <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
    );
  }

  const configuration = query.data.data;
  const selectedSettings = configuration.settings.filter((item) =>
    view === "mcp" ? item.family === "mcp" : item.family !== "mcp",
  );
  const visibleSettings = filterSettings(selectedSettings, search);
  const llmSettings = visibleSettings.filter((item) => item.family === "llm");
  const modelGroups = groupModels(visibleSettings.filter((item) => item.family === "model"));
  const mcpSettings = visibleSettings.filter((item) => item.family === "mcp");

  const edit = (item: GatewayPolicySetting) => {
    save.reset();
    setEditor(item);
  };
  const requestDelete = (item: GatewayPolicySetting) => {
    remove.reset();
    setDeleteConfirmed(false);
    setDeleteTarget(item);
  };

  return (
    <div className="stack gateway-policy-manager">
      {receipt ? <MutationReceipt receipt={receipt} /> : null}
      <div className="gateway-policy-toolbar">
        <div aria-label={t("Gateway policy family")} className="mcp-segmented" role="tablist">
          <button
            aria-controls="gateway-policy-panel"
            aria-selected={view === "llm-model"}
            className={view === "llm-model" ? "is-active" : undefined}
            id="gateway-policy-tab-llm-model"
            onClick={() => setView("llm-model")}
            onKeyDown={(event) => handleTabKey(event, view, setView)}
            role="tab"
            tabIndex={view === "llm-model" ? 0 : -1}
            type="button"
          >
            <Bot aria-hidden="true" size={15} /> {t("LLM / MODEL")}
          </button>
          <button
            aria-controls="gateway-policy-panel"
            aria-selected={view === "mcp"}
            className={view === "mcp" ? "is-active" : undefined}
            id="gateway-policy-tab-mcp"
            onClick={() => setView("mcp")}
            onKeyDown={(event) => handleTabKey(event, view, setView)}
            role="tab"
            tabIndex={view === "mcp" ? 0 : -1}
            type="button"
          >
            <Boxes aria-hidden="true" size={15} /> {t("MCP")}
          </button>
        </div>
        <label className="gateway-policy-search">
          <span className="sr-only">{t("Filter gateway policies")}</span>
          <input
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t("Filter gateway policies")}
            type="search"
            value={search}
          />
        </label>
      </div>

      <div
        aria-labelledby={view === "mcp" ? "gateway-policy-tab-mcp" : "gateway-policy-tab-llm-model"}
        id="gateway-policy-panel"
        role="tabpanel"
      >
        {!selectedSettings.length ? (
          <EmptyState
            description="The connection service did not report a verified policy catalog for this family."
            title="No gateway policies reported"
          />
        ) : !visibleSettings.length ? (
          <EmptyState
            description="No verified gateway policy matches the current filter."
            title="No policies match this filter"
          />
        ) : view === "mcp" ? (
          <div className="gateway-policy-groups">
            <PolicyCollection
              description="Top-level MCP policies retain their source-owned values and placement."
              items={mcpSettings}
              onDelete={requestDelete}
              onEdit={edit}
              title="MCP policies"
            />
          </div>
        ) : (
          <div className="gateway-policy-groups">
            {llmSettings.length ? (
              <PolicyCollection
                description="Global LLM policies applied before model selection."
                items={llmSettings}
                onDelete={requestDelete}
                onEdit={edit}
                title="Global LLM policies"
              />
            ) : null}
            {[...modelGroups.entries()].map(([target, items]) => (
              <PolicyCollection
                description="Policy fields owned by this direct model."
                items={items}
                key={target}
                onDelete={requestDelete}
                onEdit={edit}
                title={`${t("Model policies")} · ${target}`}
              />
            ))}
          </div>
        )}
      </div>

      <PolicyEditorDialog
        error={save.isError ? save.error : undefined}
        item={editor}
        onClose={() => !save.isPending && setEditor(undefined)}
        onSave={(item, value) => save.mutate({ item, value })}
        pending={save.isPending}
      />
      <PolicyDeleteDialog
        confirmed={deleteConfirmed}
        error={remove.isError ? remove.error : undefined}
        item={deleteTarget}
        onClose={() => !remove.isPending && setDeleteTarget(undefined)}
        onConfirm={() => deleteTarget && remove.mutate(deleteTarget)}
        onToggle={setDeleteConfirmed}
        pending={remove.isPending}
      />
    </div>
  );
}

function PolicyCollection({
  title,
  description,
  items,
  onEdit,
  onDelete,
}: {
  title: string;
  description: string;
  items: GatewayPolicySetting[];
  onEdit: (item: GatewayPolicySetting) => void;
  onDelete: (item: GatewayPolicySetting) => void;
}) {
  const { t } = useI18n();
  const enabled = items.filter((item) => item.enabled).length;
  const columns = useMemo<Column<GatewayPolicySetting>[]>(
    () => [
      {
        key: "policy",
        header: "Policy",
        className: "gateway-policy-column--policy",
        render: (item) => (
          <div className="primary-cell gateway-policy-name">
            <ShieldCheck aria-hidden="true" size={15} />
            <span>
              <strong>{t(item.title)}</strong>
              <small>
                <code>{item.key}</code>
                {item.description ? <span>{t(item.description)}</span> : null}
              </small>
            </span>
          </div>
        ),
      },
      {
        key: "group",
        header: "Group",
        render: (item) => <span className="gateway-policy-group">{t(item.group)}</span>,
      },
      {
        key: "placement",
        header: "Placement",
        className: "gateway-policy-column--placement",
        render: (item) => (
          <div className="gateway-policy-placement">
            <div>
              <SourceBadge source={item.source} />
              <strong>{item.target}</strong>
            </div>
            <span>
              {item.scope} · {item.phase} · {item.action}
            </span>
            <code title={item.rawRef.id}>{item.rawRef.id}</code>
          </div>
        ),
      },
      {
        key: "status",
        header: "Status",
        render: (item) => <StatusBadge status={item.enabled ? "Enabled" : "Disabled"} />,
      },
      {
        key: "value",
        header: "Value",
        className: "gateway-policy-column--value",
        render: (item) => <PolicyValue item={item} />,
      },
      {
        key: "actions",
        header: "Manage",
        className: "gateway-policy-column--actions",
        render: (item) => (
          <div className="gateway-policy-actions">
            <IconButton
              disabled={!item.editable}
              label={item.enabled ? "Edit policy" : "Configure policy"}
              name={item.title}
              onClick={() => onEdit(item)}
            >
              {item.enabled ? (
                <Pencil aria-hidden="true" size={15} />
              ) : (
                <Plus aria-hidden="true" size={15} />
              )}
            </IconButton>
            <IconButton
              danger
              disabled={!item.editable || !item.enabled}
              label="Delete policy"
              name={item.title}
              onClick={() => onDelete(item)}
            >
              <Trash2 aria-hidden="true" size={15} />
            </IconButton>
          </div>
        ),
      },
    ],
    [onDelete, onEdit, t],
  );

  return (
    <Card className="gateway-policy-card">
      <CardHeader
        action={
          <div className="gateway-policy-card__summary">
            <span>
              {enabled} {t("enabled")} · {items.length} {t("available")}
            </span>
            <SourceBadge source="agentgateway" />
          </div>
        }
        description={description}
        title={title}
      />
      <DataTable columns={columns} data={items} label={`${title} ${t("policy catalog")}`} />
    </Card>
  );
}

function PolicyValue({ item }: { item: GatewayPolicySetting }) {
  const { t } = useI18n();
  if (!item.enabled) {
    return <span className="gateway-policy-unset">{t("Not configured")}</span>;
  }
  return (
    <details className="gateway-policy-value">
      <summary>
        <Braces aria-hidden="true" size={14} /> {t("Complete value")}
      </summary>
      <pre>{jsonText(item.value)}</pre>
    </details>
  );
}

function PolicyEditorDialog({
  item,
  pending,
  error,
  onClose,
  onSave,
}: {
  item?: GatewayPolicySetting;
  pending: boolean;
  error?: unknown;
  onClose: () => void;
  onSave: (item: GatewayPolicySetting, value: unknown) => void;
}) {
  return (
    <Dialog
      description={item?.description || "Complete source-owned connection policy value."}
      onClose={onClose}
      open={Boolean(item)}
      size="wide"
      title={item ? `${item.enabled ? "Edit" : "Configure"} ${item.title}` : "Edit policy"}
    >
      {item ? (
        <PolicyEditor
          error={error}
          item={item}
          key={item.id}
          onCancel={onClose}
          onSave={onSave}
          pending={pending}
        />
      ) : null}
    </Dialog>
  );
}

function PolicyEditor({
  item,
  pending,
  error,
  onCancel,
  onSave,
}: {
  item: GatewayPolicySetting;
  pending: boolean;
  error?: unknown;
  onCancel: () => void;
  onSave: (item: GatewayPolicySetting, value: unknown) => void;
}) {
  const { t } = useI18n();
  const [value, setValue] = useState(item.enabled ? jsonText(item.value) : "");
  const [parseError, setParseError] = useState<string>();

  const submit = (event: FormEvent) => {
    event.preventDefault();
    try {
      const parsed = JSON.parse(value) as unknown;
      if (parsed === null) throw new Error(t("Policy value cannot be null."));
      setParseError(undefined);
      onSave(item, parsed);
    } catch (caught) {
      setParseError(caught instanceof Error ? caught.message : t("Invalid JSON value"));
    }
  };

  return (
    <form className="dialog-form gateway-policy-editor" onSubmit={submit}>
      <DefinitionList
        items={[
          { label: "Source", value: <SourceBadge source={item.source} /> },
          { label: "Family", value: item.family.toUpperCase() },
          { label: "Target", value: item.target },
          { label: "Scope", value: item.scope },
          { label: "Phase", value: item.phase },
          { label: "Action", value: item.action },
          { label: "Raw reference", value: <code>{item.rawRef.id}</code> },
        ]}
      />
      <label className="llm-field gateway-policy-json-field">
        <span>
          {t("Complete policy value")} <code>JSON</code>
        </span>
        <textarea
          aria-invalid={Boolean(parseError)}
          autoFocus
          onChange={(event) => setValue(event.target.value)}
          rows={16}
          spellCheck={false}
          value={value}
        />
      </label>
      {parseError ? (
        <p className="mutation-error" role="alert">
          {parseError}
        </p>
      ) : null}
      {error ? <MutationError error={error} /> : null}
      <footer>
        <Button disabled={pending} onClick={onCancel} type="button" variant="ghost">
          {t("Cancel")}
        </Button>
        <Button disabled={pending || !value.trim()} type="submit" variant="primary">
          {pending ? (
            <LoaderCircle aria-hidden="true" className="spin" size={14} />
          ) : (
            <Save aria-hidden="true" size={14} />
          )}
          {t("Save policy")}
        </Button>
      </footer>
    </form>
  );
}

function PolicyDeleteDialog({
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
      description="This removes the selected policy value from its source-owned connection configuration."
      onClose={onClose}
      open={Boolean(item)}
      title="Delete gateway policy"
    >
      {item ? (
        <div className="dialog-form gateway-policy-delete">
          <DefinitionList
            items={[
              { label: "Policy", value: item.title },
              { label: "Key", value: <code>{item.key}</code> },
              { label: "Target", value: item.target },
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
            {t("I confirm this connection policy should be removed.")}
          </label>
          {error ? <MutationError error={error} /> : null}
          <footer>
            <Button disabled={pending} onClick={onClose} type="button" variant="ghost">
              {t("Cancel")}
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
              {t("Delete policy")}
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

function MutationError({ error }: { error: unknown }) {
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

function IconButton({
  label,
  name,
  danger,
  disabled,
  onClick,
  children,
}: {
  label: string;
  name: string;
  danger?: boolean;
  disabled?: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  const { t } = useI18n();
  const accessibleLabel = `${t(label)}: ${name}`;
  return (
    <Button
      aria-label={accessibleLabel}
      disabled={disabled}
      onClick={onClick}
      size="sm"
      title={accessibleLabel}
      variant={danger ? "danger" : "ghost"}
    >
      {children}
    </Button>
  );
}

function filterSettings(settings: GatewayPolicySetting[], search: string) {
  const normalized = search.trim().toLowerCase();
  if (!normalized) return settings;
  return settings.filter((item) =>
    [
      item.title,
      item.key,
      item.group,
      item.target,
      item.description,
      item.scope,
      item.phase,
      item.action,
      item.rawRef.id,
      jsonText(item.value),
    ]
      .join(" ")
      .toLowerCase()
      .includes(normalized),
  );
}

function handleTabKey(
  event: KeyboardEvent<HTMLButtonElement>,
  current: PolicyView,
  select: (view: PolicyView) => void,
) {
  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  const next =
    event.key === "Home"
      ? "llm-model"
      : event.key === "End"
        ? "mcp"
        : current === "llm-model"
          ? "mcp"
          : "llm-model";
  select(next);
  requestAnimationFrame(() => document.getElementById(`gateway-policy-tab-${next}`)?.focus());
}

function groupModels(settings: GatewayPolicySetting[]) {
  const grouped = new Map<string, GatewayPolicySetting[]>();
  for (const item of settings) {
    const target = item.target || item.upstreamId;
    const values = grouped.get(target) ?? [];
    values.push(item);
    grouped.set(target, values);
  }
  return grouped;
}

function jsonText(value: unknown) {
  if (value === undefined) return "";
  try {
    return JSON.stringify(value, null, 2) ?? "";
  } catch {
    return String(value);
  }
}
