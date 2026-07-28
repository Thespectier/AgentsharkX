import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  CheckCircle2,
  CirclePlus,
  Network,
  Pencil,
  Plus,
  Route,
  Save,
  Server,
  Trash2,
} from "lucide-react";
import { type FormEvent, type InputHTMLAttributes, type ReactNode, useState } from "react";

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
  TrafficBindDraft,
  TrafficBindSetting,
  TrafficConfigObject,
  TrafficListenerDraft,
  TrafficListenerSetting,
  TrafficMutationReceipt,
  TrafficRouteDraft,
  TrafficRouteSetting,
} from "../../generated/api-client";
import { formatError, getScenario, mutateOperation, requestOperation } from "../../lib/api";
import { formatTimeWithZone } from "../../lib/format";
import { useI18n } from "../../lib/i18n";

type View = "listeners" | "routes";
type Editor =
  | { kind: "bind"; item?: TrafficBindSetting }
  | { kind: "listener"; item?: TrafficListenerSetting; bindId?: string }
  | { kind: "route"; item?: TrafficRouteSetting; listenerId?: string };
type DeleteTarget =
  | { kind: "bind"; item: TrafficBindSetting }
  | { kind: "listener"; item: TrafficListenerSetting }
  | { kind: "route"; item: TrafficRouteSetting };
type SaveCommand =
  | { kind: "bind"; item?: TrafficBindSetting; draft: TrafficBindDraft }
  | {
      kind: "listener";
      item?: TrafficListenerSetting;
      bindId?: string;
      draft: TrafficListenerDraft;
    }
  | {
      kind: "route";
      item?: TrafficRouteSetting;
      listenerId?: string;
      draft: TrafficRouteDraft;
    };

export function TrafficManager() {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const scenario = getScenario();
  const [view, setView] = useState<View>("listeners");
  const [search, setSearch] = useState("");
  const [editor, setEditor] = useState<Editor>();
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget>();
  const [deleteConfirmed, setDeleteConfirmed] = useState(false);
  const [receipt, setReceipt] = useState<TrafficMutationReceipt>();
  const queryKey = ["connect-traffic-configuration", scenario] as const;
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => requestOperation("getTrafficConfiguration", signal),
    retry: false,
  });

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey });
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["connect-summary"] }),
      queryClient.invalidateQueries({ queryKey: ["connect-routes"] }),
    ]);
  };

  const save = useMutation({
    mutationFn: async (command: SaveCommand) => {
      const revisionToken = query.data?.data.revisionToken;
      if (!revisionToken)
        throw new Error(t("The configuration revision is unavailable. Refresh and retry."));
      if (command.kind === "bind") {
        const body = { revisionToken, bind: command.draft };
        return command.item
          ? mutateOperation("updateTrafficBind", body, { path: { resourceId: command.item.id } })
          : mutateOperation("createTrafficBind", body);
      }
      if (command.kind === "listener") {
        const body = { revisionToken, bindId: command.bindId, listener: command.draft };
        return command.item
          ? mutateOperation("updateTrafficListener", body, {
              path: { resourceId: command.item.id },
            })
          : mutateOperation("createTrafficListener", body);
      }
      const body = { revisionToken, listenerId: command.listenerId, route: command.draft };
      return command.item
        ? mutateOperation("updateTrafficRoute", body, { path: { resourceId: command.item.id } })
        : mutateOperation("createTrafficRoute", body);
    },
    onSuccess: async (response) => {
      setReceipt(response.data);
      setEditor(undefined);
      await refresh();
    },
    onError: async () => queryClient.invalidateQueries({ queryKey }),
  });

  const remove = useMutation({
    mutationFn: async (target: DeleteTarget) => {
      const revisionToken = query.data?.data.revisionToken;
      if (!revisionToken)
        throw new Error(t("The configuration revision is unavailable. Refresh and retry."));
      const body = { revisionToken, confirmed: true, deleteChildren: true };
      if (target.kind === "bind")
        return mutateOperation("deleteTrafficBind", body, { path: { resourceId: target.item.id } });
      if (target.kind === "listener")
        return mutateOperation("deleteTrafficListener", body, {
          path: { resourceId: target.item.id },
        });
      return mutateOperation("deleteTrafficRoute", body, { path: { resourceId: target.item.id } });
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
    return <div className="resource-note">{t("Loading traffic configuration...")}</div>;
  if (query.isError || !query.data)
    return (
      <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
    );

  const configuration = query.data.data;
  const normalizedSearch = search.trim().toLowerCase();
  const listeners = configuration.listeners.filter(
    (item) =>
      !normalizedSearch ||
      `${item.name} ${item.hostname} ${item.protocol} ${item.port}`
        .toLowerCase()
        .includes(normalizedSearch),
  );
  const routes = configuration.routes.filter(
    (item) =>
      !normalizedSearch ||
      `${item.name} ${item.listener} ${item.kind} ${item.port} ${item.hostnames.join(" ")}`
        .toLowerCase()
        .includes(normalizedSearch),
  );

  return (
    <div className="stack traffic-manager">
      {receipt ? <MutationReceipt receipt={receipt} /> : null}
      <div className="traffic-manager__bar">
        <div aria-label={t("Traffic view")} className="mcp-segmented" role="radiogroup">
          <button
            aria-checked={view === "listeners"}
            className={view === "listeners" ? "is-active" : undefined}
            onClick={() => setView("listeners")}
            role="radio"
            type="button"
          >
            <Network size={15} /> {t("Listeners")}
          </button>
          <button
            aria-checked={view === "routes"}
            className={view === "routes" ? "is-active" : undefined}
            onClick={() => setView("routes")}
            role="radio"
            type="button"
          >
            <Route size={15} /> {t("Routes")}
          </button>
        </div>
        <label className="traffic-search">
          <span className="sr-only">{t("Filter traffic configuration")}</span>
          <input
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t("Filter listeners and routes")}
            value={search}
          />
        </label>
      </div>
      {view === "listeners" ? (
        <ListenersView
          binds={configuration.binds}
          listeners={listeners}
          onAddBind={() => {
            save.reset();
            setEditor({ kind: "bind" });
          }}
          onAddListener={(bindId) => {
            save.reset();
            setEditor({ kind: "listener", bindId });
          }}
          onAddRoute={(listenerId) => {
            save.reset();
            setEditor({ kind: "route", listenerId });
          }}
          onDelete={(target) => {
            remove.reset();
            setDeleteConfirmed(false);
            setDeleteTarget(target);
          }}
          onEdit={(next) => {
            save.reset();
            setEditor(next);
          }}
        />
      ) : (
        <RoutesView
          listeners={configuration.listeners}
          onAdd={() => {
            save.reset();
            setEditor({ kind: "route", listenerId: configuration.listeners[0]?.id });
          }}
          onDelete={(item) => {
            remove.reset();
            setDeleteConfirmed(false);
            setDeleteTarget({ kind: "route", item });
          }}
          onEdit={(item) => {
            save.reset();
            setEditor({ kind: "route", item });
          }}
          routes={routes}
        />
      )}
      <EditorDialog
        binds={configuration.binds}
        editor={editor}
        error={save.isError ? save.error : undefined}
        listeners={configuration.listeners}
        onClose={() => !save.isPending && setEditor(undefined)}
        onSave={(command) => save.mutate(command)}
        pending={save.isPending}
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

function ListenersView({
  binds,
  listeners,
  onAddBind,
  onAddListener,
  onAddRoute,
  onEdit,
  onDelete,
}: {
  binds: TrafficBindSetting[];
  listeners: TrafficListenerSetting[];
  onAddBind: () => void;
  onAddListener: (bindId: string) => void;
  onAddRoute: (listenerId: string) => void;
  onEdit: (editor: Editor) => void;
  onDelete: (target: DeleteTarget) => void;
}) {
  const { t } = useI18n();
  return (
    <Card>
      <CardHeader
        action={
          <Button onClick={onAddBind} size="sm" variant="ghost">
            <Plus size={15} /> {t("Add bind")}
          </Button>
        }
        description="Bind ports and complete HTTP, HTTPS, HBONE, TCP, and TLS listener configuration."
        title="Traffic listeners"
      />
      {!binds.length ? (
        <EmptyState
          action={
            <Button onClick={onAddBind} variant="primary">
              <Network size={15} /> {t("Add bind")}
            </Button>
          }
          description="Add a bind port before attaching listeners and routes."
          title="No traffic binds configured"
        />
      ) : (
        <div className="traffic-bind-list">
          {binds.map((bind) => {
            const items = listeners.filter((listener) => listener.bindId === bind.id);
            return (
              <section className="traffic-bind" key={bind.id}>
                <header>
                  <div>
                    <strong>:{bind.port}</strong>
                    <span>
                      {bind.listenerCount} {t("listeners")} · {bind.routeCount} {t("routes")} ·{" "}
                      {bind.backendCount} {t("backends")}
                    </span>
                  </div>
                  <div className="llm-row-actions">
                    <StatusBadge status={bind.tunnelProtocol} />
                    <IconButton label="Add listener" onClick={() => onAddListener(bind.id)}>
                      <Plus size={15} />
                    </IconButton>
                    <IconButton
                      label="Edit bind"
                      onClick={() => onEdit({ kind: "bind", item: bind })}
                    >
                      <Pencil size={15} />
                    </IconButton>
                    <IconButton
                      danger
                      label="Delete bind"
                      onClick={() => onDelete({ kind: "bind", item: bind })}
                    >
                      <Trash2 size={15} />
                    </IconButton>
                  </div>
                </header>
                {!items.length ? (
                  <p className="resource-note">{t("No matching listeners on this bind.")}</p>
                ) : (
                  <ListenerTable
                    items={items}
                    onAddRoute={onAddRoute}
                    onDelete={(item) => onDelete({ kind: "listener", item })}
                    onEdit={(item) => onEdit({ kind: "listener", item })}
                  />
                )}
              </section>
            );
          })}
        </div>
      )}
    </Card>
  );
}

function ListenerTable({
  items,
  onAddRoute,
  onEdit,
  onDelete,
}: {
  items: TrafficListenerSetting[];
  onAddRoute: (listenerId: string) => void;
  onEdit: (item: TrafficListenerSetting) => void;
  onDelete: (item: TrafficListenerSetting) => void;
}) {
  const columns: Column<TrafficListenerSetting>[] = [
    {
      key: "name",
      header: "Listener",
      render: (item) => (
        <div className="primary-cell">
          <Server size={15} />
          <span>
            <strong>{item.name}</strong>
            <small>{item.hostname || "*"}</small>
          </span>
        </div>
      ),
    },
    {
      key: "protocol",
      header: "Protocol",
      render: (item) => <StatusBadge status={item.protocol} />,
    },
    { key: "routes", header: "Routes", render: (item) => item.routeCount },
    { key: "backends", header: "Backends", render: (item) => item.backendCount },
    { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
    {
      key: "actions",
      header: "Actions",
      render: (item) => (
        <div className="llm-row-actions">
          <IconButton label="Add route" onClick={() => onAddRoute(item.id)}>
            <CirclePlus size={15} />
          </IconButton>
          <IconButton label="Edit listener" onClick={() => onEdit(item)}>
            <Pencil size={15} />
          </IconButton>
          <IconButton danger label="Delete listener" onClick={() => onDelete(item)}>
            <Trash2 size={15} />
          </IconButton>
        </div>
      ),
    },
  ];
  return <DataTable columns={columns} data={items} label="Traffic listeners" />;
}

function RoutesView({
  routes,
  listeners,
  onAdd,
  onEdit,
  onDelete,
}: {
  routes: TrafficRouteSetting[];
  listeners: TrafficListenerSetting[];
  onAdd: () => void;
  onEdit: (item: TrafficRouteSetting) => void;
  onDelete: (item: TrafficRouteSetting) => void;
}) {
  const { t } = useI18n();
  const columns: Column<TrafficRouteSetting>[] = [
    {
      key: "name",
      header: "Route",
      render: (item) => (
        <div className="primary-cell">
          <Route size={15} />
          <span>
            <strong>{item.name}</strong>
            <small>{routeMatchSummary(item)}</small>
          </span>
        </div>
      ),
    },
    {
      key: "kind",
      header: "Type",
      render: (item) => <StatusBadge status={item.kind.toUpperCase()} />,
    },
    {
      key: "listener",
      header: "Listener",
      render: (item) => (
        <code>
          {item.listener}:{item.port}
        </code>
      ),
    },
    { key: "backends", header: "Backends", render: (item) => item.backendCount },
    { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
    {
      key: "actions",
      header: "Actions",
      render: (item) => (
        <div className="llm-row-actions">
          <IconButton label="Edit route" onClick={() => onEdit(item)}>
            <Pencil size={15} />
          </IconButton>
          <IconButton danger label="Delete route" onClick={() => onDelete(item)}>
            <Trash2 size={15} />
          </IconButton>
        </div>
      ),
    },
  ];
  return (
    <Card>
      <CardHeader
        action={
          <Button disabled={!listeners.length} onClick={onAdd} size="sm" variant="primary">
            <Plus size={15} /> {t("Add route")}
          </Button>
        }
        description="Complete HTTP and TCP matches, backends, weights, and source-owned policies."
        title="Traffic routes"
      />
      {routes.length ? (
        <DataTable columns={columns} data={routes} label="Traffic routes" />
      ) : (
        <EmptyState
          description={
            listeners.length
              ? "Add a route under an HTTP or TCP listener."
              : "Add a listener before creating routes."
          }
          title="No traffic routes configured"
        />
      )}
    </Card>
  );
}

function EditorDialog({
  editor,
  binds,
  listeners,
  pending,
  error,
  onClose,
  onSave,
}: {
  editor?: Editor;
  binds: TrafficBindSetting[];
  listeners: TrafficListenerSetting[];
  pending: boolean;
  error?: unknown;
  onClose: () => void;
  onSave: (command: SaveCommand) => void;
}) {
  const title = editor
    ? `${editor.item ? "Edit" : "Add"} ${editor.kind}`
    : "Edit traffic configuration";
  return (
    <Dialog
      description="Changes are written to the verified agentgateway configuration and checked after save."
      onClose={onClose}
      open={Boolean(editor)}
      size="wide"
      title={title}
    >
      {editor?.kind === "bind" ? (
        <BindEditor
          error={error}
          item={editor.item}
          key={editor.item?.id ?? "new-bind"}
          onCancel={onClose}
          onSave={(draft) => onSave({ kind: "bind", item: editor.item, draft })}
          pending={pending}
        />
      ) : editor?.kind === "listener" ? (
        <ListenerEditor
          binds={binds}
          error={error}
          item={editor.item}
          key={editor.item?.id ?? `new-listener-${editor.bindId}`}
          onCancel={onClose}
          onSave={(bindId, draft) => onSave({ kind: "listener", item: editor.item, bindId, draft })}
          pending={pending}
          selectedBindId={editor.bindId}
        />
      ) : editor?.kind === "route" ? (
        <RouteEditor
          error={error}
          item={editor.item}
          key={editor.item?.id ?? `new-route-${editor.listenerId}`}
          listeners={listeners}
          onCancel={onClose}
          onSave={(listenerId, draft) =>
            onSave({ kind: "route", item: editor.item, listenerId, draft })
          }
          pending={pending}
          selectedListenerId={editor.listenerId}
        />
      ) : null}
    </Dialog>
  );
}

function BindEditor({
  item,
  pending,
  error,
  onCancel,
  onSave,
}: {
  item?: TrafficBindSetting;
  pending: boolean;
  error?: unknown;
  onCancel: () => void;
  onSave: (draft: TrafficBindDraft) => void;
}) {
  const [port, setPort] = useState(item?.port.toString() ?? "8080");
  const numericPort = Number(port);
  const valid = Number.isInteger(numericPort) && numericPort >= 1 && numericPort <= 65535;
  return (
    <form
      className="dialog-form llm-form"
      onSubmit={(event) => {
        event.preventDefault();
        if (valid) onSave({ port: numericPort });
      }}
    >
      <TextField
        label="Port"
        max={65535}
        min={1}
        onChange={setPort}
        required
        type="number"
        value={port}
      />
      {item ? (
        <p className="llm-form-note">
          {item.listenerCount} listeners remain attached to this bind.
        </p>
      ) : null}
      {error ? <MutationError error={error} /> : null}
      <FormFooter label="Save bind" onCancel={onCancel} pending={pending} valid={valid} />
    </form>
  );
}

function ListenerEditor({
  item,
  binds,
  selectedBindId,
  pending,
  error,
  onCancel,
  onSave,
}: {
  item?: TrafficListenerSetting;
  binds: TrafficBindSetting[];
  selectedBindId?: string;
  pending: boolean;
  error?: unknown;
  onCancel: () => void;
  onSave: (bindId: string, draft: TrafficListenerDraft) => void;
}) {
  const { t } = useI18n();
  const initial = structuredClone(item?.configuration ?? { protocol: "HTTP" });
  const [bindId, setBindId] = useState(item?.bindId ?? selectedBindId ?? binds[0]?.id ?? "");
  const [name, setName] = useState(stringValue(initial.name));
  const [namespace, setNamespace] = useState(stringValue(initial.namespace));
  const [hostname, setHostname] = useState(stringValue(initial.hostname));
  const [protocol, setProtocol] = useState(stringValue(initial.protocol) || "HTTP");
  const [tls, setTLS] = useState(jsonText(initial.tls));
  const [policies, setPolicies] = useState(jsonText(initial.policies));
  const [deleteRoutes, setDeleteRoutes] = useState(false);
  const [parseError, setParseError] = useState<string>();
  const changingKind = Boolean(item && listenerKind(item.protocol) !== listenerKind(protocol));

  const submit = (event: FormEvent) => {
    event.preventDefault();
    try {
      const configuration: TrafficConfigObject = { ...initial, protocol };
      setOptionalString(configuration, "name", name);
      setOptionalString(configuration, "namespace", namespace);
      setOptionalString(configuration, "hostname", hostname);
      setOptionalJSON(configuration, "tls", tls);
      setOptionalJSON(configuration, "policies", policies);
      delete configuration.routes;
      delete configuration.tcpRoutes;
      setParseError(undefined);
      onSave(bindId, { configuration, deleteIncompatibleRoutes: changingKind && deleteRoutes });
    } catch (caught) {
      setParseError(caught instanceof Error ? caught.message : t("Invalid JSON value"));
    }
  };

  return (
    <form className="dialog-form llm-form traffic-editor" onSubmit={submit}>
      {!item ? (
        <SelectField
          label="Bind"
          onChange={setBindId}
          options={binds.map((bind) => ({ label: `:${bind.port}`, value: bind.id }))}
          value={bindId}
        />
      ) : null}
      <div className="llm-form-grid">
        <TextField label="Name" onChange={setName} placeholder="public-http" value={name} />
        <TextField
          label="Namespace"
          onChange={setNamespace}
          placeholder="default"
          value={namespace}
        />
        <TextField label="Hostname" onChange={setHostname} placeholder="*" value={hostname} />
        <SelectField
          label="Protocol"
          onChange={setProtocol}
          options={["HTTP", "HTTPS", "HBONE", "TCP", "TLS"].map((value) => ({
            label: value,
            value,
          }))}
          value={protocol}
        />
      </div>
      <div className="traffic-json-grid">
        <JSONField label="TLS configuration" onChange={setTLS} value={tls} />
        <JSONField label="Listener policies" onChange={setPolicies} value={policies} />
      </div>
      {changingKind && item?.routeCount ? (
        <label className="confirm-field traffic-protocol-warning">
          <input
            checked={deleteRoutes}
            onChange={(event) => setDeleteRoutes(event.target.checked)}
            type="checkbox"
          />
          {t(`Remove ${item.routeCount} incompatible routes when changing protocol family`)}
        </label>
      ) : null}
      {parseError ? <p className="mutation-error">{parseError}</p> : null}
      {error ? <MutationError error={error} /> : null}
      <FormFooter
        label="Save listener"
        onCancel={onCancel}
        pending={pending}
        valid={Boolean(bindId) && (!changingKind || !item?.routeCount || deleteRoutes)}
      />
    </form>
  );
}

function RouteEditor({
  item,
  listeners,
  selectedListenerId,
  pending,
  error,
  onCancel,
  onSave,
}: {
  item?: TrafficRouteSetting;
  listeners: TrafficListenerSetting[];
  selectedListenerId?: string;
  pending: boolean;
  error?: unknown;
  onCancel: () => void;
  onSave: (listenerId: string, draft: TrafficRouteDraft) => void;
}) {
  const { t } = useI18n();
  const [listenerId, setListenerId] = useState(
    item?.listenerId ?? selectedListenerId ?? listeners[0]?.id ?? "",
  );
  const selected = listeners.find((listener) => listener.id === listenerId);
  const kind = item?.kind ?? listenerKind(selected?.protocol ?? "HTTP");
  const initial = structuredClone(
    item?.configuration ??
      (kind === "http"
        ? { matches: [{ path: { pathPrefix: "/" } }], backends: [] }
        : { backends: [] }),
  );
  const [name, setName] = useState(stringValue(initial.name));
  const [namespace, setNamespace] = useState(stringValue(initial.namespace));
  const [ruleName, setRuleName] = useState(stringValue(initial.ruleName));
  const [hostnames, setHostnames] = useState(stringArray(initial.hostnames).join(", "));
  const [matches, setMatches] = useState(
    jsonText(initial.matches ?? [{ path: { pathPrefix: "/" } }]),
  );
  const [backends, setBackends] = useState(jsonText(initial.backends ?? []));
  const [policies, setPolicies] = useState(jsonText(initial.policies));
  const [parseError, setParseError] = useState<string>();

  const addBackend = (type: string) => {
    try {
      const values = parseJSONArray(backends, "Backends");
      const next: Record<string, unknown> =
        type === "service"
          ? { service: { name: "default/service", port: 80 } }
          : type === "dynamic"
            ? { dynamic: {} }
            : { [type]: type === "host" ? "localhost:8080" : "backend-name" };
      if (type === "routeGroup") next.routeGroup = "shared-routes";
      setBackends(JSON.stringify([...values, next], null, 2));
      setParseError(undefined);
    } catch (caught) {
      setParseError(caught instanceof Error ? caught.message : t("Invalid JSON value"));
    }
  };

  const submit = (event: FormEvent) => {
    event.preventDefault();
    try {
      const configuration: TrafficConfigObject = { ...initial };
      setOptionalString(configuration, "name", name);
      setOptionalString(configuration, "namespace", namespace);
      setOptionalString(configuration, "ruleName", ruleName);
      configuration.hostnames = splitList(hostnames);
      configuration.backends = parseJSONArray(backends, "Backends");
      if (kind === "http") configuration.matches = parseJSONArray(matches, "Matches");
      else delete configuration.matches;
      setOptionalJSON(configuration, "policies", policies);
      setParseError(undefined);
      onSave(listenerId, { kind, configuration });
    } catch (caught) {
      setParseError(caught instanceof Error ? caught.message : t("Invalid JSON value"));
    }
  };

  return (
    <form className="dialog-form llm-form traffic-editor" onSubmit={submit}>
      {!item ? (
        <SelectField
          label="Listener"
          onChange={setListenerId}
          options={listeners.map((listener) => ({
            label: `:${listener.port} · ${listener.name} · ${listenerKind(listener.protocol).toUpperCase()}`,
            value: listener.id,
          }))}
          value={listenerId}
        />
      ) : null}
      <div className="llm-form-grid">
        <TextField label="Name" onChange={setName} placeholder="api" value={name} />
        <TextField label="Rule name" onChange={setRuleName} value={ruleName} />
        <TextField
          label="Namespace"
          onChange={setNamespace}
          placeholder="default"
          value={namespace}
        />
        <TextField
          label="Hostnames"
          onChange={setHostnames}
          placeholder="example.com, *.example.com"
          value={hostnames}
        />
      </div>
      {kind === "http" ? (
        <JSONField label="HTTP matches" onChange={setMatches} rows={10} value={matches} />
      ) : null}
      <fieldset className="llm-fieldset traffic-backends">
        <legend>{t("Backends")}</legend>
        <div className="traffic-backend-actions">
          {[
            ["host", "Host"],
            ["service", "Service"],
            ["backend", "Backend reference"],
            ...(kind === "http"
              ? [
                  ["dynamic", "Dynamic"],
                  ["routeGroup", "Route group"],
                ]
              : []),
          ].map(([value, label]) => (
            <Button
              key={value}
              onClick={() => addBackend(value)}
              size="sm"
              type="button"
              variant="ghost"
            >
              <Plus size={13} /> {t(label)}
            </Button>
          ))}
        </div>
        <JSONField
          label="Backend configuration"
          onChange={setBackends}
          rows={12}
          value={backends}
        />
      </fieldset>
      <JSONField label="Route policies" onChange={setPolicies} rows={8} value={policies} />
      {parseError ? <p className="mutation-error">{parseError}</p> : null}
      {error ? <MutationError error={error} /> : null}
      <FormFooter
        label="Save route"
        onCancel={onCancel}
        pending={pending}
        valid={Boolean(listenerId)}
      />
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
  target?: DeleteTarget;
  confirmed: boolean;
  pending: boolean;
  error?: unknown;
  onClose: () => void;
  onToggle: (confirmed: boolean) => void;
  onConfirm: () => void;
}) {
  const { t } = useI18n();
  const counts =
    target && target.kind !== "route"
      ? [
          { label: "Routes", value: target.item.routeCount },
          { label: "Backends", value: target.item.backendCount },
        ]
      : [];
  return (
    <Dialog
      description="This removes the selected source-owned configuration and any children shown below."
      onClose={onClose}
      open={Boolean(target)}
      title={`Delete traffic ${target?.kind ?? "resource"}`}
    >
      {target ? (
        <div className="dialog-form">
          <DefinitionList
            items={[
              { label: "Resource", value: deleteName(target) },
              ...counts,
              { label: "Source", value: "agentgateway" },
            ]}
          />
          <label className="confirm-field">
            <input
              checked={confirmed}
              onChange={(event) => onToggle(event.target.checked)}
              type="checkbox"
            />
            {t("I understand this configuration will be removed")}
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
              <Trash2 size={14} /> Delete {target.kind}
            </Button>
          </footer>
        </div>
      ) : null}
    </Dialog>
  );
}

function FormFooter({
  label,
  valid,
  pending,
  onCancel,
}: {
  label: string;
  valid: boolean;
  pending: boolean;
  onCancel: () => void;
}) {
  return (
    <footer>
      <Button disabled={pending} onClick={onCancel} type="button" variant="ghost">
        Cancel
      </Button>
      <Button disabled={pending || !valid} type="submit" variant="primary">
        <Save size={14} /> {label}
      </Button>
    </footer>
  );
}

function TextField({
  label,
  value,
  onChange,
  ...input
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
} & Omit<InputHTMLAttributes<HTMLInputElement>, "value" | "onChange">) {
  const { t } = useI18n();
  return (
    <label className="llm-field">
      <span>{t(label)}</span>
      <input {...input} onChange={(event) => onChange(event.target.value)} value={value} />
    </label>
  );
}

function SelectField({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: Array<{ label: string; value: string }>;
  onChange: (value: string) => void;
}) {
  const { t } = useI18n();
  return (
    <label className="llm-field">
      <span>{t(label)}</span>
      <select onChange={(event) => onChange(event.target.value)} value={value}>
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {t(option.label)}
          </option>
        ))}
      </select>
    </label>
  );
}

function JSONField({
  label,
  value,
  onChange,
  rows = 7,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  rows?: number;
}) {
  const { t } = useI18n();
  return (
    <label className="llm-field traffic-json-field">
      <span>
        {t(label)} <code>JSON</code>
      </span>
      <textarea
        onChange={(event) => onChange(event.target.value)}
        rows={rows}
        spellCheck={false}
        value={value}
      />
    </label>
  );
}

function IconButton({
  label,
  danger,
  onClick,
  children,
}: {
  label: string;
  danger?: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  const { t } = useI18n();
  return (
    <Button
      aria-label={t(label)}
      onClick={onClick}
      size="sm"
      title={t(label)}
      variant={danger ? "danger" : "ghost"}
    >
      {children}
    </Button>
  );
}

function MutationReceipt({ receipt }: { receipt: TrafficMutationReceipt }) {
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

function routeMatchSummary(item: TrafficRouteSetting) {
  if (item.kind === "tcp") return item.hostnames.join(", ") || "All TCP traffic";
  const matches = Array.isArray(item.configuration.matches) ? item.configuration.matches : [];
  const first = matches[0] as { path?: Record<string, unknown>; method?: string } | undefined;
  const path = first?.path ? Object.values(first.path)[0] : "/";
  return [first?.method, typeof path === "string" ? path : "/"].filter(Boolean).join(" ");
}

function deleteName(target: DeleteTarget) {
  if (target.kind === "bind") return `:${target.item.port}`;
  return target.item.name;
}

function listenerKind(protocol: string): "http" | "tcp" {
  return protocol === "TCP" || protocol === "TLS" ? "tcp" : "http";
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value : "";
}

function stringArray(value: unknown) {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : [];
}

function splitList(value: string) {
  return value
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function jsonText(value: unknown) {
  return JSON.stringify(value ?? {}, null, 2);
}

function parseJSONObject(value: string, label: string) {
  const parsed = JSON.parse(value || "{}") as unknown;
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed))
    throw new Error(`${label} must be a JSON object.`);
  return parsed as Record<string, unknown>;
}

function parseJSONArray(value: string, label: string) {
  const parsed = JSON.parse(value || "[]") as unknown;
  if (!Array.isArray(parsed)) throw new Error(`${label} must be a JSON array.`);
  if (parsed.some((item) => !item || typeof item !== "object" || Array.isArray(item)))
    throw new Error(`${label} entries must be JSON objects.`);
  return parsed as Array<Record<string, unknown>>;
}

function setOptionalString(configuration: TrafficConfigObject, key: string, value: string) {
  if (value.trim()) configuration[key] = value.trim();
  else delete configuration[key];
}

function setOptionalJSON(configuration: TrafficConfigObject, key: string, value: string) {
  const parsed = parseJSONObject(value, key);
  if (Object.keys(parsed).length) configuration[key] = parsed;
  else delete configuration[key];
}
