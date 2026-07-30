import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  CheckCircle2,
  Pencil,
  Plus,
  Save,
  Settings2,
  TerminalSquare,
  Trash2,
  Waypoints,
} from "lucide-react";
import { type FormEvent, type InputHTMLAttributes, useState } from "react";

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
  GatewayMCPServer,
  McpGlobalSettings,
  McpGlobalSettingsDraft,
  McpMutationReceipt,
  McpNetworkTarget,
  McpServerDraft,
  McpServerSetting,
  McpStdioTarget,
} from "../../generated/api-client";
import { formatTimeWithZone } from "../../lib/format";
import { formatError, getScenario, mutateOperation, requestOperation } from "../../lib/api";
import { useI18n } from "../../lib/i18n";

type Editor = { item?: McpServerSetting };

export function McpManager() {
  const { t } = useI18n();
  const scenario = getScenario();
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const [editor, setEditor] = useState<Editor>();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<McpServerSetting>();
  const [deleteConfirmed, setDeleteConfirmed] = useState(false);
  const [receipt, setReceipt] = useState<McpMutationReceipt>();
  const queryKey = ["connect-mcp-configuration", scenario] as const;
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => requestOperation("getMcpConfiguration", signal),
    retry: false,
  });

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey });
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["connect-summary"] }),
      queryClient.invalidateQueries({ queryKey: ["connect-mcp"] }),
    ]);
  };

  const saveServer = useMutation({
    mutationFn: async ({ item, draft }: { item?: McpServerSetting; draft: McpServerDraft }) => {
      const revisionToken = query.data?.data.revisionToken;
      if (!revisionToken)
        throw new Error(t("The configuration revision is unavailable. Refresh and retry."));
      return item
        ? mutateOperation(
            "updateMcpServer",
            { revisionToken, server: draft },
            { path: { resourceId: item.id } },
          )
        : mutateOperation("createMcpServer", { revisionToken, server: draft });
    },
    onSuccess: async (response) => {
      setReceipt(response.data);
      setEditor(undefined);
      await refresh();
    },
    onError: async () => queryClient.invalidateQueries({ queryKey }),
  });

  const saveSettings = useMutation({
    mutationFn: async (settings: McpGlobalSettingsDraft) => {
      const revisionToken = query.data?.data.revisionToken;
      if (!revisionToken)
        throw new Error(t("The configuration revision is unavailable. Refresh and retry."));
      return mutateOperation("updateMcpSettings", { revisionToken, settings });
    },
    onSuccess: async (response) => {
      setReceipt(response.data);
      setSettingsOpen(false);
      await refresh();
    },
    onError: async () => queryClient.invalidateQueries({ queryKey }),
  });

  const remove = useMutation({
    mutationFn: async (item: McpServerSetting) => {
      const revisionToken = query.data?.data.revisionToken;
      if (!revisionToken)
        throw new Error(t("The configuration revision is unavailable. Refresh and retry."));
      return mutateOperation(
        "deleteMcpServer",
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

  if (query.isLoading)
    return <div className="resource-note">{t("Loading MCP configuration...")}</div>;
  if (query.isError || !query.data)
    return (
      <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
    );

  const configuration = query.data.data;
  const normalizedSearch = search.trim().toLowerCase();
  const servers = configuration.servers.filter(
    (item) =>
      !normalizedSearch ||
      item.name.toLowerCase().includes(normalizedSearch) ||
      item.transport.toLowerCase().includes(normalizedSearch) ||
      endpoint(item).toLowerCase().includes(normalizedSearch),
  );

  return (
    <div className="stack mcp-manager">
      {receipt ? <MutationReceipt receipt={receipt} /> : null}
      <McpSettingsCard
        settings={configuration.settings}
        onEdit={() => {
          saveSettings.reset();
          setSettingsOpen(true);
        }}
      />
      <McpServersCard
        items={servers}
        onAdd={() => {
          saveServer.reset();
          setEditor({});
        }}
        onDelete={(item) => {
          remove.reset();
          setDeleteConfirmed(false);
          setDeleteTarget(item);
        }}
        onEdit={(item) => {
          saveServer.reset();
          setEditor({ item });
        }}
        search={search}
        setSearch={setSearch}
      />
      <AdvancedTargets inlineServers={configuration.inlineServers} />
      <ServerEditorDialog
        editor={editor}
        error={saveServer.isError ? saveServer.error : undefined}
        onClose={() => !saveServer.isPending && setEditor(undefined)}
        onSave={(draft) => saveServer.mutate({ item: editor?.item, draft })}
        pending={saveServer.isPending}
      />
      <SettingsDialog
        error={saveSettings.isError ? saveSettings.error : undefined}
        onClose={() => !saveSettings.isPending && setSettingsOpen(false)}
        onSave={(settings) => saveSettings.mutate(settings)}
        open={settingsOpen}
        pending={saveSettings.isPending}
        settings={configuration.settings}
      />
      <DeleteDialog
        confirmed={deleteConfirmed}
        error={remove.isError ? remove.error : undefined}
        onClose={() => !remove.isPending && setDeleteTarget(undefined)}
        onConfirm={() => deleteTarget && remove.mutate(deleteTarget)}
        onToggle={setDeleteConfirmed}
        pending={remove.isPending}
        target={deleteTarget}
      />
    </div>
  );
}

function McpSettingsCard({
  settings,
  onEdit,
}: {
  settings: McpGlobalSettings;
  onEdit: () => void;
}) {
  return (
    <Card>
      <CardHeader
        action={
          <Button aria-label="Edit MCP settings" onClick={onEdit} size="sm" variant="ghost">
            <Settings2 size={15} /> Edit settings
          </Button>
        }
        description="Listener and federation behavior from verified Agentshark Connection fields."
        title="MCP settings"
      />
      <DefinitionList
        items={[
          { label: "Port", value: settings.port === null ? "Not configured" : settings.port },
          { label: "Session state", value: settings.statefulMode },
          { label: "Tool prefix", value: settings.prefixMode },
          { label: "Failure mode", value: settings.failureMode },
          {
            label: "Advanced policy",
            value: settings.hasPolicies ? "Configured" : "Not configured",
          },
        ]}
      />
    </Card>
  );
}

function McpServersCard({
  items,
  search,
  setSearch,
  onAdd,
  onEdit,
  onDelete,
}: {
  items: McpServerSetting[];
  search: string;
  setSearch: (value: string) => void;
  onAdd: () => void;
  onEdit: (item: McpServerSetting) => void;
  onDelete: (item: McpServerSetting) => void;
}) {
  const { t } = useI18n();
  const columns: Column<McpServerSetting>[] = [
    {
      key: "name",
      header: "MCP server",
      render: (item) => (
        <div className="primary-cell">
          {item.transport === "stdio" ? <TerminalSquare size={15} /> : <Waypoints size={15} />}
          <span>
            <strong>{item.name}</strong>
            <small>{item.upstreamId}</small>
          </span>
        </div>
      ),
    },
    {
      key: "transport",
      header: "Transport",
      render: (item) => <StatusBadge status={transportLabel(item.transport)} />,
    },
    { key: "endpoint", header: "Endpoint", render: (item) => <code>{endpoint(item)}</code> },
    {
      key: "policy",
      header: "Policy",
      render: (item) => (item.hasPolicies ? t("Configured") : t("None")),
    },
    { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
    {
      key: "actions",
      header: "Actions",
      render: (item) => (
        <div className="llm-row-actions">
          <Button
            aria-label={t(
              item.editable ? "Edit MCP server" : "OpenAPI targets use advanced configuration",
            )}
            disabled={!item.editable}
            onClick={() => onEdit(item)}
            size="sm"
            title={t(
              item.editable ? "Edit MCP server" : "OpenAPI targets use advanced configuration",
            )}
            variant="ghost"
          >
            <Pencil size={14} />
          </Button>
          <Button
            aria-label={t(
              item.editable ? "Delete MCP server" : "OpenAPI targets use advanced configuration",
            )}
            disabled={!item.editable}
            onClick={() => onDelete(item)}
            size="sm"
            title={t(
              item.editable ? "Delete MCP server" : "OpenAPI targets use advanced configuration",
            )}
            variant="ghost"
          >
            <Trash2 size={14} />
          </Button>
        </div>
      ),
    },
  ];
  return (
    <Card>
      <CardHeader
        action={
          <Button onClick={onAdd} size="sm" variant="primary">
            <Plus size={14} /> Add server
          </Button>
        }
        description="Top-level MCP Tools served through Agentshark Connection."
        title="MCP servers"
      />
      <div className="resource-toolbar llm-toolbar">
        <label>
          <span className="sr-only">{t("Filter MCP servers")}</span>
          <input
            aria-label={t("Filter MCP servers")}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t("Filter MCP servers")}
            value={search}
          />
        </label>
        <span>{t("{count} shown", { count: items.length })}</span>
      </div>
      {items.length ? (
        <DataTable columns={columns} data={items} label="MCP server settings" />
      ) : (
        <EmptyState
          compact
          description="No MCP server matches this filter."
          title="No MCP servers found"
        />
      )}
    </Card>
  );
}

function AdvancedTargets({ inlineServers }: { inlineServers: GatewayMCPServer[] }) {
  const columns: Column<GatewayMCPServer>[] = [
    { key: "name", header: "MCP server", render: (item) => <strong>{item.name}</strong> },
    {
      key: "transport",
      header: "Transport",
      render: (item) => <StatusBadge status={transportLabel(item.transport)} />,
    },
    { key: "scope", header: "Owning route", render: (item) => <code>{item.scope}</code> },
    { key: "fetched", header: "Fetched", render: (item) => formatTimeWithZone(item.fetchedAt) },
  ];
  return (
    <Card>
      <CardHeader
        description="Route-owned MCP targets, OpenAPI schemas, and policy bodies remain with their owning advanced configuration."
        title="Route and advanced targets"
      />
      {inlineServers.length ? (
        <DataTable columns={columns} data={inlineServers} label="Inline route MCP targets" />
      ) : (
        <EmptyState
          compact
          description="No route-owned MCP targets are configured."
          title="No inline targets"
        />
      )}
    </Card>
  );
}

function ServerEditorDialog({
  editor,
  pending,
  error,
  onClose,
  onSave,
}: {
  editor?: Editor;
  pending: boolean;
  error?: unknown;
  onClose: () => void;
  onSave: (draft: McpServerDraft) => void;
}) {
  return (
    <Dialog
      description="Verified MCP transport fields are written while target policies and unrelated configuration are preserved."
      onClose={onClose}
      open={Boolean(editor)}
      size="wide"
      title={editor?.item ? "Edit MCP server" : "Add MCP server"}
    >
      {editor ? (
        <ServerEditor
          error={error}
          item={editor.item}
          key={editor.item?.id ?? "new"}
          onCancel={onClose}
          onSave={onSave}
          pending={pending}
        />
      ) : null}
    </Dialog>
  );
}

function ServerEditor({
  item,
  pending,
  error,
  onCancel,
  onSave,
}: {
  item?: McpServerSetting;
  pending: boolean;
  error?: unknown;
  onCancel: () => void;
  onSave: (draft: McpServerDraft) => void;
}) {
  const { t } = useI18n();
  const [name, setName] = useState(item?.name ?? "");
  const [transport, setTransport] = useState<McpServerDraft["transport"]>(
    item?.transport === "sse" || item?.transport === "stdio" ? item.transport : "mcp",
  );
  const [network, setNetwork] = useState<McpNetworkTarget>(
    item?.network ?? { mode: "url", host: "http://localhost:3001/mcp" },
  );
  const [command, setCommand] = useState(item?.stdio?.command ?? "");
  const [argumentsText, setArgumentsText] = useState(
    JSON.stringify(item?.stdio?.arguments ?? [], null, 2),
  );
  const [environmentText, setEnvironmentText] = useState(
    JSON.stringify(item?.stdio?.environment ?? {}, null, 2),
  );
  const [clearEnvironment, setClearEnvironment] = useState(item?.stdio?.clearEnvironment ?? false);
  const [parseError, setParseError] = useState<string>();
  const validName = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$/.test(name);
  const networkValid =
    network.mode === "url"
      ? Boolean(network.host)
      : network.mode === "host"
        ? Boolean(
            network.host && network.port !== null && network.port !== undefined && network.path,
          )
        : Boolean(network.backend);
  const valid = validName && (transport === "stdio" ? Boolean(command) : networkValid);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!valid) return;
    setParseError(undefined);
    if (transport === "stdio") {
      try {
        const stdio: McpStdioTarget = {
          command,
          arguments: parseStringArray(argumentsText),
          environment: parseStringMap(environmentText),
          clearEnvironment,
        };
        onSave({ name, transport, stdio });
      } catch (caught) {
        setParseError(caught instanceof Error ? caught.message : t("Invalid JSON value"));
      }
      return;
    }
    onSave({ name, transport, network: normalizeNetwork(network) });
  };

  return (
    <form className="dialog-form llm-form" onSubmit={submit}>
      <div className="llm-form-grid">
        <TextField
          label="Server name"
          maxLength={253}
          onChange={setName}
          pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*"
          placeholder="weather"
          required
          value={name}
        />
      </div>
      <fieldset className="llm-fieldset">
        <legend>{t("Transport")}</legend>
        <SegmentedControl
          label="Transport"
          onChange={(value) => {
            setTransport(value as McpServerDraft["transport"]);
            if (value === "sse" && network.mode === "url" && network.host?.endsWith("/mcp"))
              setNetwork({ ...network, host: `${network.host.slice(0, -4)}/sse` });
            if (value === "mcp" && network.mode === "url" && network.host?.endsWith("/sse"))
              setNetwork({ ...network, host: `${network.host.slice(0, -4)}/mcp` });
          }}
          options={[
            { value: "mcp", label: "Streamable HTTP" },
            { value: "sse", label: "Legacy SSE" },
            { value: "stdio", label: "Command line" },
          ]}
          value={transport}
        />
      </fieldset>
      {transport === "stdio" ? (
        <fieldset className="llm-fieldset">
          <legend>{t("Process")}</legend>
          <div className="llm-form-grid">
            <TextField
              label="Command"
              maxLength={4096}
              onChange={setCommand}
              placeholder="npx"
              required
              value={command}
            />
            <CheckboxField
              checked={clearEnvironment}
              label="Clear inherited environment"
              onChange={setClearEnvironment}
            />
            <TextAreaField
              label="Arguments (JSON array)"
              onChange={setArgumentsText}
              value={argumentsText}
            />
            <TextAreaField
              label="Environment (JSON object)"
              onChange={setEnvironmentText}
              value={environmentText}
            />
          </div>
        </fieldset>
      ) : (
        <fieldset className="llm-fieldset">
          <legend>{t("Connection")}</legend>
          <SegmentedControl
            label="Connection mode"
            onChange={(mode) =>
              setNetwork(networkForMode(mode as McpNetworkTarget["mode"], network, transport))
            }
            options={[
              { value: "url", label: "URL" },
              { value: "host", label: "Host + port" },
              { value: "backend", label: "Backend reference" },
            ]}
            value={network.mode}
          />
          <div className="llm-form-grid mcp-connection-grid">
            {network.mode === "url" ? (
              <TextField
                label="URL"
                maxLength={2048}
                onChange={(host) => setNetwork({ mode: "url", host })}
                placeholder={
                  transport === "sse" ? "http://localhost:3001/sse" : "http://localhost:3001/mcp"
                }
                required
                type="url"
                value={network.host ?? ""}
                wide
              />
            ) : network.mode === "host" ? (
              <>
                <TextField
                  label="Host"
                  maxLength={2048}
                  onChange={(host) => setNetwork({ ...network, host })}
                  required
                  value={network.host ?? ""}
                />
                <TextField
                  label="Port"
                  max={65535}
                  min={0}
                  onChange={(value) =>
                    setNetwork({ ...network, port: value === "" ? null : Number(value) })
                  }
                  required
                  type="number"
                  value={network.port?.toString() ?? ""}
                />
                <TextField
                  label="Path"
                  maxLength={2048}
                  onChange={(path) => setNetwork({ ...network, path })}
                  placeholder={transport === "sse" ? "/sse" : "/mcp"}
                  required
                  value={network.path ?? ""}
                  wide
                />
              </>
            ) : (
              <>
                <TextField
                  label="Backend reference"
                  maxLength={256}
                  onChange={(backend) => setNetwork({ ...network, backend })}
                  required
                  value={network.backend ?? ""}
                />
                <TextField
                  label="Path"
                  maxLength={2048}
                  onChange={(path) => setNetwork({ ...network, path })}
                  placeholder={transport === "sse" ? "/sse" : "/mcp"}
                  value={network.path ?? ""}
                />
              </>
            )}
          </div>
        </fieldset>
      )}
      {parseError ? <p className="mutation-error">{parseError}</p> : null}
      {error ? <MutationError error={error} /> : null}
      <footer>
        <Button disabled={pending} onClick={onCancel} type="button" variant="ghost">
          Cancel
        </Button>
        <Button disabled={pending || !valid} type="submit" variant="primary">
          <Save size={14} /> Save server
        </Button>
      </footer>
    </form>
  );
}

function SettingsDialog({
  open,
  settings,
  pending,
  error,
  onClose,
  onSave,
}: {
  open: boolean;
  settings: McpGlobalSettings;
  pending: boolean;
  error?: unknown;
  onClose: () => void;
  onSave: (settings: McpGlobalSettingsDraft) => void;
}) {
  return (
    <Dialog
      description="Update the verified top-level MCP listener and federation behavior."
      onClose={onClose}
      open={open}
      title="Edit MCP settings"
    >
      {open ? (
        <SettingsEditor
          error={error}
          key={`${settings.port}:${settings.statefulMode}:${settings.prefixMode}:${settings.failureMode}`}
          onCancel={onClose}
          onSave={onSave}
          pending={pending}
          settings={settings}
        />
      ) : null}
    </Dialog>
  );
}

function SettingsEditor({
  settings,
  pending,
  error,
  onCancel,
  onSave,
}: {
  settings: McpGlobalSettings;
  pending: boolean;
  error?: unknown;
  onCancel: () => void;
  onSave: (settings: McpGlobalSettingsDraft) => void;
}) {
  const { t } = useI18n();
  const [draft, setDraft] = useState<McpGlobalSettingsDraft>({
    port: settings.port,
    statefulMode: settings.statefulMode,
    prefixMode: settings.prefixMode,
    failureMode: settings.failureMode,
  });
  return (
    <form
      className="dialog-form llm-form"
      onSubmit={(event) => {
        event.preventDefault();
        onSave(draft);
      }}
    >
      <TextField
        label="Port"
        max={65535}
        min={0}
        onChange={(value) => setDraft({ ...draft, port: value === "" ? null : Number(value) })}
        placeholder="3001"
        type="number"
        value={draft.port?.toString() ?? ""}
      />
      <fieldset className="llm-fieldset">
        <legend>{t("Session state")}</legend>
        <SegmentedControl
          label="Session state"
          onChange={(statefulMode) =>
            setDraft({
              ...draft,
              statefulMode: statefulMode as McpGlobalSettingsDraft["statefulMode"],
            })
          }
          options={[
            { value: "stateless", label: "Stateless" },
            { value: "stateful", label: "Stateful" },
          ]}
          value={draft.statefulMode}
        />
      </fieldset>
      <fieldset className="llm-fieldset">
        <legend>{t("Tool prefix")}</legend>
        <SegmentedControl
          label="Tool prefix"
          onChange={(prefixMode) =>
            setDraft({ ...draft, prefixMode: prefixMode as McpGlobalSettingsDraft["prefixMode"] })
          }
          options={[
            { value: "none", label: "None" },
            { value: "always", label: "Always" },
            { value: "conditional", label: "Conditional" },
          ]}
          value={draft.prefixMode}
        />
      </fieldset>
      <fieldset className="llm-fieldset">
        <legend>{t("Failure mode")}</legend>
        <SegmentedControl
          label="Failure mode"
          onChange={(failureMode) =>
            setDraft({
              ...draft,
              failureMode: failureMode as McpGlobalSettingsDraft["failureMode"],
            })
          }
          options={[
            { value: "failClosed", label: "Fail closed" },
            { value: "failOpen", label: "Fail open" },
          ]}
          value={draft.failureMode}
        />
      </fieldset>
      {error ? <MutationError error={error} /> : null}
      <footer>
        <Button disabled={pending} onClick={onCancel} type="button" variant="ghost">
          Cancel
        </Button>
        <Button disabled={pending} type="submit" variant="primary">
          <Save size={14} /> Save settings
        </Button>
      </footer>
    </form>
  );
}

function DeleteDialog({
  target,
  confirmed,
  pending,
  error,
  onClose,
  onToggle,
  onConfirm,
}: {
  target?: McpServerSetting;
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
      description="This removes the top-level target from Agentshark Connection."
      onClose={onClose}
      open={Boolean(target)}
      title="Delete MCP server"
    >
      {target ? (
        <div className="dialog-form">
          <DefinitionList
            items={[
              { label: "MCP server", value: target.name },
              { label: "Transport", value: transportLabel(target.transport) },
              { label: "Endpoint", value: <code>{endpoint(target)}</code> },
            ]}
          />
          <label className="confirm-field">
            <input
              checked={confirmed}
              onChange={(event) => onToggle(event.target.checked)}
              type="checkbox"
            />
            {t("I understand this target will be removed")}
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
              <Trash2 size={14} /> Delete server
            </Button>
          </footer>
        </div>
      ) : null}
    </Dialog>
  );
}

function SegmentedControl({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: Array<{ value: string; label: string }>;
  onChange: (value: string) => void;
}) {
  const { t } = useI18n();
  return (
    <div aria-label={t(label)} className="mcp-segmented" role="radiogroup">
      {options.map((option) => (
        <button
          aria-checked={value === option.value}
          className={value === option.value ? "is-active" : undefined}
          key={option.value}
          onClick={() => onChange(option.value)}
          role="radio"
          type="button"
        >
          {t(option.label)}
        </button>
      ))}
    </div>
  );
}

function TextField({
  label,
  value,
  onChange,
  wide,
  ...input
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  wide?: boolean;
} & Omit<InputHTMLAttributes<HTMLInputElement>, "value" | "onChange">) {
  const { t } = useI18n();
  return (
    <label className={`llm-field${wide ? " llm-field--wide" : ""}`}>
      <span>{t(label)}</span>
      <input {...input} onChange={(event) => onChange(event.target.value)} value={value} />
    </label>
  );
}

function TextAreaField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const { t } = useI18n();
  return (
    <label className="llm-field">
      <span>{t(label)}</span>
      <textarea
        onChange={(event) => onChange(event.target.value)}
        rows={6}
        spellCheck={false}
        value={value}
      />
    </label>
  );
}

function CheckboxField({
  checked,
  label,
  onChange,
}: {
  checked: boolean;
  label: string;
  onChange: (checked: boolean) => void;
}) {
  const { t } = useI18n();
  return (
    <label className="llm-checkbox-field">
      <input
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
        type="checkbox"
      />
      {t(label)}
    </label>
  );
}

function MutationReceipt({ receipt }: { receipt: McpMutationReceipt }) {
  const { t } = useI18n();
  return (
    <Card className="mutation-receipt">
      <CheckCircle2 size={17} />
      <div>
        <strong>{t(receipt.message)}</strong>
        <span>
          {receipt.target} · {formatTimeWithZone(receipt.completedAt)}
        </span>
      </div>
    </Card>
  );
}

function MutationError({ error }: { error: unknown }) {
  return <p className="mutation-error">{formatError(error)}</p>;
}

function transportLabel(transport: string) {
  if (transport === "mcp") return "Streamable HTTP";
  if (transport === "sse") return "Legacy SSE";
  if (transport === "stdio") return "Command line";
  return "OpenAPI";
}

function endpoint(item: McpServerSetting) {
  if (item.stdio) return [item.stdio.command, ...item.stdio.arguments].join(" ");
  if (!item.network) return "Not provided";
  if (item.network.mode === "url") return item.network.host ?? "Not provided";
  if (item.network.mode === "host")
    return `${item.network.host ?? ""}:${item.network.port ?? ""}${item.network.path ?? ""}`;
  return `backend:${item.network.backend ?? ""}${item.network.path ?? ""}`;
}

function networkForMode(
  mode: McpNetworkTarget["mode"],
  current: McpNetworkTarget,
  transport: McpServerDraft["transport"],
): McpNetworkTarget {
  const path = transport === "sse" ? "/sse" : "/mcp";
  if (mode === "url")
    return { mode, host: current.mode === "url" ? current.host : `http://localhost:3001${path}` };
  if (mode === "host")
    return {
      mode,
      host: current.host && !current.host.includes("://") ? current.host : "localhost",
      port: current.port ?? 3001,
      path: current.path ?? path,
    };
  return { mode, backend: current.backend ?? "", path: current.path ?? path };
}

function normalizeNetwork(network: McpNetworkTarget): McpNetworkTarget {
  if (network.mode === "url") return { mode: "url", host: network.host };
  if (network.mode === "host")
    return { mode: "host", host: network.host, port: network.port, path: network.path };
  return { mode: "backend", backend: network.backend, path: network.path || undefined };
}

function parseStringArray(value: string): string[] {
  const parsed: unknown = JSON.parse(value);
  if (!Array.isArray(parsed) || parsed.some((item) => typeof item !== "string"))
    throw new Error("Arguments must be a JSON string array.");
  return parsed;
}

function parseStringMap(value: string): Record<string, string> {
  const parsed: unknown = JSON.parse(value);
  if (
    !parsed ||
    typeof parsed !== "object" ||
    Array.isArray(parsed) ||
    Object.values(parsed).some((item) => typeof item !== "string")
  )
    throw new Error("Environment must be a JSON string object.");
  return parsed as Record<string, string>;
}
