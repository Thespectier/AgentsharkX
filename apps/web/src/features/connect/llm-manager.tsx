import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  CheckCircle2,
  KeyRound,
  Network,
  Pencil,
  Plus,
  Save,
  ServerCog,
  Trash2,
} from "lucide-react";
import { type FormEvent, useMemo, useState } from "react";

import {
  Button,
  Card,
  CardHeader,
  DataTable,
  Dialog,
  EmptyState,
  ErrorState,
  ExternalButton,
  SourceBadge,
  StatusBadge,
  type Column,
} from "../../components/ui";
import type {
  GatewayModel,
  LlmCredentialInput,
  LlmModelDraft,
  LlmModelSetting,
  LlmMutationReceipt,
  LlmProviderDraft,
  LlmProviderFormat,
  LlmProviderSetting,
  LlmProviderType,
} from "../../generated/api-client";
import { formatTimeWithZone } from "../../lib/format";
import { formatError, getScenario, mutateOperation, requestOperation } from "../../lib/api";
import { useI18n } from "../../lib/i18n";

const providerTypes: LlmProviderType[] = [
  "openai",
  "anthropic",
  "gemini",
  "vertex",
  "bedrock",
  "azure",
  "copilot",
  "cohere",
  "ollama",
  "baseten",
  "cerebras",
  "deepinfra",
  "deepseek",
  "groq",
  "huggingface",
  "mistral",
  "openrouter",
  "togetherai",
  "xai",
  "fireworks",
  "custom",
];

const formatTypes: LlmProviderFormat["type"][] = [
  "completions",
  "messages",
  "responses",
  "embeddings",
  "anthropicTokenCount",
  "realtime",
  "rerank",
];

type Editor =
  { kind: "provider"; item?: LlmProviderSetting } | { kind: "model"; item?: LlmModelSetting };

type DeleteTarget =
  { kind: "provider"; item: LlmProviderSetting } | { kind: "model"; item: LlmModelSetting };

type SaveTarget =
  | { kind: "provider"; item?: LlmProviderSetting; draft: LlmProviderDraft }
  | { kind: "model"; item?: LlmModelSetting; draft: LlmModelDraft };

export function LlmManager() {
  const { t } = useI18n();
  const scenario = getScenario();
  const queryClient = useQueryClient();
  const [editor, setEditor] = useState<Editor>();
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget>();
  const [deleteConfirmed, setDeleteConfirmed] = useState(false);
  const [providerSearch, setProviderSearch] = useState("");
  const [modelSearch, setModelSearch] = useState("");
  const [receipt, setReceipt] = useState<LlmMutationReceipt>();
  const queryKey = ["connect-llm-configuration", scenario] as const;
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => requestOperation("getLlmConfiguration", signal),
    retry: false,
  });

  const refreshConfiguration = async () => {
    await queryClient.invalidateQueries({ queryKey });
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["connect-summary"] }),
      queryClient.invalidateQueries({ queryKey: ["connect-providers"] }),
      queryClient.invalidateQueries({ queryKey: ["connect-models"] }),
    ]);
  };

  const save = useMutation({
    mutationFn: async (target: SaveTarget) => {
      const revisionToken = query.data?.data.revisionToken;
      if (!revisionToken)
        throw new Error(t("The configuration revision is unavailable. Refresh and retry."));
      if (target.kind === "provider") {
        return target.item
          ? mutateOperation(
              "updateLlmProvider",
              { revisionToken, provider: target.draft },
              { path: { resourceId: target.item.id } },
            )
          : mutateOperation("createLlmProvider", { revisionToken, provider: target.draft });
      }
      return target.item
        ? mutateOperation(
            "updateLlmModel",
            { revisionToken, model: target.draft },
            { path: { resourceId: target.item.id } },
          )
        : mutateOperation("createLlmModel", { revisionToken, model: target.draft });
    },
    onSuccess: async (response) => {
      setReceipt(response.data);
      setEditor(undefined);
      await refreshConfiguration();
    },
    onError: async () => {
      await queryClient.invalidateQueries({ queryKey });
    },
  });

  const remove = useMutation({
    mutationFn: async (target: DeleteTarget) => {
      const revisionToken = query.data?.data.revisionToken;
      if (!revisionToken)
        throw new Error(t("The configuration revision is unavailable. Refresh and retry."));
      const body = { revisionToken, confirmed: true };
      return target.kind === "provider"
        ? mutateOperation("deleteLlmProvider", body, { path: { resourceId: target.item.id } })
        : mutateOperation("deleteLlmModel", body, { path: { resourceId: target.item.id } });
    },
    onSuccess: async (response) => {
      setReceipt(response.data);
      setDeleteTarget(undefined);
      setDeleteConfirmed(false);
      await refreshConfiguration();
    },
    onError: async () => {
      await queryClient.invalidateQueries({ queryKey });
    },
  });

  const openEditor = (next: Editor) => {
    save.reset();
    setEditor(next);
  };
  const openDelete = (next: DeleteTarget) => {
    remove.reset();
    setDeleteConfirmed(false);
    setDeleteTarget(next);
  };

  if (query.isLoading)
    return <div className="resource-note">{t("Loading LLM configuration...")}</div>;
  if (query.isError || !query.data) {
    return (
      <ErrorState description={formatError(query.error)} onRetry={() => void query.refetch()} />
    );
  }

  const configuration = query.data.data;
  const providers = configuration.providers.filter((item) =>
    item.name.toLowerCase().includes(providerSearch.trim().toLowerCase()),
  );
  const models = configuration.models.filter((item) => {
    const queryValue = modelSearch.trim().toLowerCase();
    return (
      item.name.toLowerCase().includes(queryValue) ||
      (item.providerReference ?? item.providerType ?? "").toLowerCase().includes(queryValue)
    );
  });
  const virtualReferences = new Set(
    configuration.virtualModels.flatMap((item) => item.targets ?? []),
  );

  return (
    <div className="stack llm-manager">
      {receipt ? <MutationReceipt receipt={receipt} /> : null}
      <ProviderSettings
        items={providers}
        onAdd={() => openEditor({ kind: "provider" })}
        onDelete={(item) => openDelete({ kind: "provider", item })}
        onEdit={(item) => openEditor({ kind: "provider", item })}
        search={providerSearch}
        setSearch={setProviderSearch}
      />
      <ModelSettings
        items={models}
        onAdd={() => openEditor({ kind: "model" })}
        onDelete={(item) => openDelete({ kind: "model", item })}
        onEdit={(item) => openEditor({ kind: "model", item })}
        search={modelSearch}
        setSearch={setModelSearch}
        virtualReferences={virtualReferences}
      />
      <VirtualModels
        items={configuration.virtualModels}
        nativeHref={configuration.links.rawConfig ?? configuration.links.console}
      />
      <EditorDialog
        editor={editor}
        error={save.isError ? save.error : undefined}
        pending={save.isPending}
        providers={configuration.providers}
        onClose={() => !save.isPending && setEditor(undefined)}
        onSave={(target) => save.mutate(target)}
        virtualReferences={virtualReferences}
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

function ProviderSettings({
  items,
  search,
  setSearch,
  onAdd,
  onEdit,
  onDelete,
}: {
  items: LlmProviderSetting[];
  search: string;
  setSearch: (value: string) => void;
  onAdd: () => void;
  onEdit: (item: LlmProviderSetting) => void;
  onDelete: (item: LlmProviderSetting) => void;
}) {
  const columns: Column<LlmProviderSetting>[] = [
    {
      key: "name",
      header: "Provider",
      render: (item) => <Primary icon={ServerCog} title={item.name} subtitle={item.upstreamId} />,
    },
    { key: "type", header: "Type", render: (item) => <StatusBadge status={item.providerType} /> },
    {
      key: "credential",
      header: "Credential",
      render: (item) => <CredentialStatus item={item} />,
    },
    { key: "models", header: "Models", render: (item) => item.modelCount },
    { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
    {
      key: "actions",
      header: "Actions",
      render: (item) => (
        <RowActions
          deleteDisabled={item.modelCount > 0 || !item.editable}
          deleteTitle={
            item.modelCount > 0 ? "Provider is referenced by a model" : "Delete provider"
          }
          editDisabled={!item.editable}
          onDelete={() => onDelete(item)}
          onEdit={() => onEdit(item)}
        />
      ),
    },
  ];
  return (
    <Card>
      <CardHeader
        action={
          <Button onClick={onAdd} size="sm" variant="primary">
            <Plus size={14} /> Add provider
          </Button>
        }
        description="Shared providers and credential references from verified agentgateway fields."
        title="Providers"
      />
      <SearchBar
        count={items.length}
        label="Filter providers"
        onChange={setSearch}
        value={search}
      />
      {items.length ? (
        <DataTable columns={columns} data={items} label="LLM provider settings" />
      ) : (
        <EmptyState
          compact
          description="No provider matches this filter."
          title="No providers found"
        />
      )}
    </Card>
  );
}

function ModelSettings({
  items,
  search,
  setSearch,
  virtualReferences,
  onAdd,
  onEdit,
  onDelete,
}: {
  items: LlmModelSetting[];
  search: string;
  setSearch: (value: string) => void;
  virtualReferences: Set<string>;
  onAdd: () => void;
  onEdit: (item: LlmModelSetting) => void;
  onDelete: (item: LlmModelSetting) => void;
}) {
  const columns: Column<LlmModelSetting>[] = [
    {
      key: "name",
      header: "Model",
      render: (item) => (
        <Primary
          icon={Network}
          title={item.name}
          subtitle={item.params.model ?? "No target override"}
        />
      ),
    },
    {
      key: "provider",
      header: "Provider",
      render: (item) => (
        <code>{item.providerReference ?? item.providerType ?? item.providerMode}</code>
      ),
    },
    {
      key: "visibility",
      header: "Visibility",
      render: (item) => <StatusBadge status={item.visibility} />,
    },
    {
      key: "credential",
      header: "Credential",
      render: (item) => <CredentialStatus item={item} />,
    },
    { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
    {
      key: "actions",
      header: "Actions",
      render: (item) => {
        const referenced = virtualReferences.has(item.name);
        return (
          <RowActions
            deleteDisabled={referenced || !item.editable}
            deleteTitle={referenced ? "Model is referenced by a virtual model" : "Delete model"}
            editDisabled={!item.editable}
            onDelete={() => onDelete(item)}
            onEdit={() => onEdit(item)}
          />
        );
      },
    },
  ];
  return (
    <Card>
      <CardHeader
        action={
          <Button onClick={onAdd} size="sm" variant="primary">
            <Plus size={14} /> Add model
          </Button>
        }
        description="Direct model aliases and provider bindings. Provider binding is fixed after creation."
        title="Direct models"
      />
      <SearchBar count={items.length} label="Filter models" onChange={setSearch} value={search} />
      {items.length ? (
        <DataTable columns={columns} data={items} label="Direct LLM model settings" />
      ) : (
        <EmptyState
          compact
          description="No direct model matches this filter."
          title="No models found"
        />
      )}
    </Card>
  );
}

function VirtualModels({ items, nativeHref }: { items: GatewayModel[]; nativeHref?: string }) {
  const { t } = useI18n();
  const columns: Column<GatewayModel>[] = [
    {
      key: "name",
      header: "Virtual model",
      render: (item) => (
        <Primary icon={Network} title={item.name} subtitle={item.upstreamId ?? item.id} />
      ),
    },
    {
      key: "routing",
      header: "Routing",
      render: (item) => <StatusBadge status={item.routing ?? "unknown"} />,
    },
    {
      key: "targets",
      header: "Targets",
      render: (item) => <code>{item.targets?.join(", ") || t("Not provided")}</code>,
    },
    { key: "fetched", header: "Fetched", render: (item) => formatTimeWithZone(item.fetchedAt) },
  ];
  return (
    <Card>
      <CardHeader
        action={
          nativeHref ? (
            <ExternalButton href={nativeHref}>Advanced configuration</ExternalButton>
          ) : undefined
        }
        description="Virtual routing, failover, and policy fields remain read-only here."
        title="Virtual models"
      />
      {items.length ? (
        <DataTable columns={columns} data={items} label="Virtual LLM models" />
      ) : (
        <EmptyState
          compact
          description="agentgateway has no virtual models configured."
          title="No virtual models"
        />
      )}
    </Card>
  );
}

function SearchBar({
  value,
  onChange,
  label,
  count,
}: {
  value: string;
  onChange: (value: string) => void;
  label: string;
  count: number;
}) {
  const { t } = useI18n();
  return (
    <div className="resource-toolbar llm-toolbar">
      <label>
        <span className="sr-only">{t(label)}</span>
        <input
          aria-label={t(label)}
          onChange={(event) => onChange(event.target.value)}
          placeholder={t(label)}
          value={value}
        />
      </label>
      <span>{t("{count} shown", { count })}</span>
    </div>
  );
}

function RowActions({
  onEdit,
  onDelete,
  editDisabled,
  deleteDisabled,
  deleteTitle,
}: {
  onEdit: () => void;
  onDelete: () => void;
  editDisabled: boolean;
  deleteDisabled: boolean;
  deleteTitle: string;
}) {
  const { t } = useI18n();
  return (
    <div className="llm-row-actions">
      <Button
        aria-label={t("Edit")}
        disabled={editDisabled}
        onClick={onEdit}
        size="sm"
        title={t("Edit")}
        variant="ghost"
      >
        <Pencil size={14} />
      </Button>
      <Button
        aria-label={t(deleteTitle)}
        disabled={deleteDisabled}
        onClick={onDelete}
        size="sm"
        title={t(deleteTitle)}
        variant="ghost"
      >
        <Trash2 size={14} />
      </Button>
    </div>
  );
}

function CredentialStatus({ item }: { item: { credential: { configured: boolean } } }) {
  const { t } = useI18n();
  return (
    <span className="llm-credential-state">
      <KeyRound size={13} /> {t(item.credential.configured ? "Configured" : "Ambient / none")}
    </span>
  );
}

function Primary({
  icon: Icon,
  title,
  subtitle,
}: {
  icon: typeof Network;
  title: string;
  subtitle: string;
}) {
  const { t } = useI18n();
  return (
    <div className="primary-cell">
      <Icon size={15} />
      <span>
        <strong>{title}</strong>
        <small>{t(subtitle)}</small>
      </span>
    </div>
  );
}

function EditorDialog({
  editor,
  providers,
  virtualReferences,
  pending,
  error,
  onClose,
  onSave,
}: {
  editor?: Editor;
  providers: LlmProviderSetting[];
  virtualReferences: Set<string>;
  pending: boolean;
  error?: unknown;
  onClose: () => void;
  onSave: (target: SaveTarget) => void;
}) {
  const { t } = useI18n();
  const title =
    editor?.kind === "provider"
      ? `${editor.item ? "Edit" : "Add"} provider`
      : `${editor?.item ? "Edit" : "Add"} model`;
  return (
    <Dialog
      description="Only verified base settings are written. Existing credentials can be preserved without exposing their value."
      onClose={onClose}
      open={Boolean(editor)}
      size="wide"
      title={t(title)}
    >
      {editor?.kind === "provider" ? (
        <ProviderEditor
          key={`provider:${editor.item?.id ?? "new"}`}
          error={error}
          item={editor.item}
          onCancel={onClose}
          onSave={(draft) => onSave({ kind: "provider", item: editor.item, draft })}
          pending={pending}
        />
      ) : editor?.kind === "model" ? (
        <ModelEditor
          key={`model:${editor.item?.id ?? "new"}`}
          error={error}
          item={editor.item}
          onCancel={onClose}
          onSave={(draft) => onSave({ kind: "model", item: editor.item, draft })}
          pending={pending}
          providers={providers}
          referenced={Boolean(editor.item && virtualReferences.has(editor.item.name))}
        />
      ) : null}
    </Dialog>
  );
}

function ProviderEditor({
  item,
  pending,
  error,
  onCancel,
  onSave,
}: {
  item?: LlmProviderSetting;
  pending: boolean;
  error?: unknown;
  onCancel: () => void;
  onSave: (draft: LlmProviderDraft) => void;
}) {
  const [draft, setDraft] = useState<LlmProviderDraft>(() => ({
    name: item?.name ?? "",
    providerType: item?.providerType ?? "openai",
    params: item?.params ?? {},
    formats: item?.formats ?? [],
    credential: { mode: item ? "preserve" : "ambient" },
  }));
  const custom = draft.providerType === "custom";
  const valid =
    draft.name.trim() === draft.name &&
    Boolean(draft.name) &&
    (!custom || Boolean(draft.params.baseUrl && draft.formats.length));
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (valid) onSave(draft);
  };
  return (
    <form className="dialog-form llm-form" onSubmit={submit}>
      <div className="llm-form-grid">
        <TextField
          disabled={Boolean(item && item.modelCount > 0)}
          label="Provider name"
          maxLength={256}
          onChange={(name) => setDraft({ ...draft, name })}
          required
          value={draft.name}
        />
        <SelectField
          disabled={Boolean(item)}
          label="Provider type"
          onChange={(providerType) =>
            setDraft({
              ...draft,
              providerType: providerType as LlmProviderType,
              params: paramsForProviderType(draft.params, providerType as LlmProviderType),
              formats: providerType === "custom" ? draft.formats : [],
              credential:
                (providerType === "bedrock" || providerType === "vertex") &&
                (draft.credential.mode === "environment" || draft.credential.mode === "file")
                  ? { mode: "ambient" }
                  : draft.credential,
            })
          }
          options={providerTypes}
          value={draft.providerType}
        />
        <TextField
          label="Default model"
          maxLength={256}
          onChange={(model) => setDraft({ ...draft, params: { ...draft.params, model } })}
          value={draft.params.model ?? ""}
        />
        <TextField
          label="Base URL"
          maxLength={2048}
          onChange={(baseUrl) => setDraft({ ...draft, params: { ...draft.params, baseUrl } })}
          placeholder={custom ? "https://provider.example/v1" : "Optional override"}
          required={custom}
          type="url"
          value={draft.params.baseUrl ?? ""}
        />
        <ProviderSpecificFields
          params={draft.params}
          providerType={draft.providerType}
          setParams={(params) => setDraft({ ...draft, params })}
        />
      </div>
      {custom ? (
        <FormatFields
          formats={draft.formats}
          onChange={(formats) => setDraft({ ...draft, formats })}
        />
      ) : null}
      <CredentialFields
        configured={item?.credential.configured ?? false}
        credential={draft.credential}
        editing={Boolean(item)}
        onChange={(credential) => setDraft({ ...draft, credential })}
        providerType={draft.providerType}
      />
      {error ? <MutationError error={error} /> : null}
      <footer>
        <Button disabled={pending} onClick={onCancel} type="button" variant="ghost">
          Cancel
        </Button>
        <Button disabled={pending || !valid} type="submit" variant="primary">
          <Save size={14} /> Save provider
        </Button>
      </footer>
    </form>
  );
}

function ModelEditor({
  item,
  providers,
  referenced,
  pending,
  error,
  onCancel,
  onSave,
}: {
  item?: LlmModelSetting;
  providers: LlmProviderSetting[];
  referenced: boolean;
  pending: boolean;
  error?: unknown;
  onCancel: () => void;
  onSave: (draft: LlmModelDraft) => void;
}) {
  const { t } = useI18n();
  const [draft, setDraft] = useState<LlmModelDraft>(() => ({
    name: item?.name ?? "",
    providerMode: item?.providerMode ?? (providers.length ? "reference" : "builtin"),
    providerType: item?.providerType ?? (providers.length ? undefined : "openai"),
    providerReference:
      item?.providerReference ?? (providers.length ? providers[0].name : undefined),
    params: item?.params ?? {},
    formats: item?.formats ?? [],
    visibility: item?.visibility ?? "public",
    credential: { mode: item ? "preserve" : "ambient" },
  }));
  const valid =
    Boolean(draft.name) &&
    draft.name.trim() === draft.name &&
    (draft.providerMode !== "reference" || Boolean(draft.providerReference)) &&
    (draft.providerMode !== "custom" || Boolean(draft.params.baseUrl && draft.formats.length));
  const setMode = (providerMode: LlmModelDraft["providerMode"]) => {
    const providerType =
      providerMode === "reference"
        ? undefined
        : providerMode === "custom"
          ? "custom"
          : draft.providerType === "custom" || !draft.providerType
            ? "openai"
            : draft.providerType;
    setDraft({
      ...draft,
      providerMode,
      providerReference:
        providerMode === "reference" ? (draft.providerReference ?? providers[0]?.name) : undefined,
      providerType,
      formats: providerMode === "custom" ? draft.formats : [],
      params:
        providerMode === "reference"
          ? { model: draft.params.model }
          : paramsForProviderType(draft.params, providerType ?? "openai"),
      credential:
        providerMode === "reference" ? { mode: item ? "preserve" : "ambient" } : draft.credential,
    });
  };
  return (
    <form
      className="dialog-form llm-form"
      onSubmit={(event) => {
        event.preventDefault();
        if (valid) onSave(draft);
      }}
    >
      <div className="llm-form-grid">
        <TextField
          disabled={Boolean(item && referenced)}
          label="Model name"
          maxLength={256}
          onChange={(name) => setDraft({ ...draft, name })}
          required
          value={draft.name}
        />
        <SelectField
          disabled={Boolean(item)}
          label="Provider mode"
          onChange={(value) => setMode(value as LlmModelDraft["providerMode"])}
          options={["reference", "builtin", "custom"]}
          value={draft.providerMode}
        />
        {draft.providerMode === "reference" ? (
          <SelectField
            disabled={Boolean(item)}
            label="Shared provider"
            onChange={(providerReference) => setDraft({ ...draft, providerReference })}
            options={providers.map((provider) => provider.name)}
            value={draft.providerReference ?? ""}
          />
        ) : (
          <SelectField
            disabled={Boolean(item)}
            label="Provider type"
            onChange={(providerType) =>
              setDraft({
                ...draft,
                providerType: providerType as LlmProviderType,
                params: paramsForProviderType(draft.params, providerType as LlmProviderType),
                credential:
                  (providerType === "bedrock" || providerType === "vertex") &&
                  (draft.credential.mode === "environment" || draft.credential.mode === "file")
                    ? { mode: "ambient" }
                    : draft.credential,
              })
            }
            options={
              draft.providerMode === "custom"
                ? ["custom"]
                : providerTypes.filter((value) => value !== "custom")
            }
            value={draft.providerType ?? "openai"}
          />
        )}
        <SelectField
          label="Visibility"
          onChange={(visibility) =>
            setDraft({ ...draft, visibility: visibility as LlmModelDraft["visibility"] })
          }
          options={["public", "internal"]}
          value={draft.visibility}
        />
        <TextField
          label="Target model"
          maxLength={256}
          onChange={(model) => setDraft({ ...draft, params: { ...draft.params, model } })}
          placeholder="Optional upstream model override"
          value={draft.params.model ?? ""}
        />
        {draft.providerMode !== "reference" ? (
          <TextField
            label="Base URL"
            maxLength={2048}
            onChange={(baseUrl) => setDraft({ ...draft, params: { ...draft.params, baseUrl } })}
            placeholder={
              draft.providerMode === "custom" ? "https://provider.example/v1" : "Optional override"
            }
            required={draft.providerMode === "custom"}
            type="url"
            value={draft.params.baseUrl ?? ""}
          />
        ) : null}
        {draft.providerMode !== "reference" && draft.providerType ? (
          <ProviderSpecificFields
            params={draft.params}
            providerType={draft.providerType}
            setParams={(params) => setDraft({ ...draft, params })}
          />
        ) : null}
      </div>
      {draft.providerMode === "custom" ? (
        <FormatFields
          formats={draft.formats}
          onChange={(formats) => setDraft({ ...draft, formats })}
        />
      ) : null}
      {draft.providerMode !== "reference" ? (
        <CredentialFields
          configured={item?.credential.configured ?? false}
          credential={draft.credential}
          editing={Boolean(item)}
          onChange={(credential) => setDraft({ ...draft, credential })}
          providerType={draft.providerType}
        />
      ) : (
        <p className="llm-form-note">
          {t("Credentials are inherited from the shared provider reference.")}
        </p>
      )}
      {error ? <MutationError error={error} /> : null}
      <footer>
        <Button disabled={pending} onClick={onCancel} type="button" variant="ghost">
          Cancel
        </Button>
        <Button disabled={pending || !valid} type="submit" variant="primary">
          <Save size={14} /> Save model
        </Button>
      </footer>
    </form>
  );
}

function ProviderSpecificFields({
  providerType,
  params,
  setParams,
}: {
  providerType: LlmProviderType;
  params: LlmProviderDraft["params"];
  setParams: (params: LlmProviderDraft["params"]) => void;
}) {
  if (providerType === "bedrock")
    return (
      <TextField
        label="AWS region"
        maxLength={128}
        onChange={(awsRegion) => setParams({ ...params, awsRegion })}
        value={params.awsRegion ?? ""}
      />
    );
  if (providerType === "vertex")
    return (
      <>
        <TextField
          label="Vertex project"
          maxLength={256}
          onChange={(vertexProject) => setParams({ ...params, vertexProject })}
          value={params.vertexProject ?? ""}
        />
        <TextField
          label="Vertex region"
          maxLength={128}
          onChange={(vertexRegion) => setParams({ ...params, vertexRegion })}
          value={params.vertexRegion ?? ""}
        />
      </>
    );
  if (providerType === "azure")
    return (
      <>
        <TextField
          label="Azure resource name"
          maxLength={256}
          onChange={(azureResourceName) => setParams({ ...params, azureResourceName })}
          value={params.azureResourceName ?? ""}
        />
        <SelectField
          label="Azure resource type"
          onChange={(azureResourceType) =>
            setParams({ ...params, azureResourceType: azureResourceType as "openAI" | "foundry" })
          }
          options={["openAI", "foundry"]}
          value={params.azureResourceType ?? "openAI"}
        />
        <TextField
          label="Azure API version"
          maxLength={128}
          onChange={(azureApiVersion) => setParams({ ...params, azureApiVersion })}
          value={params.azureApiVersion ?? ""}
        />
        <TextField
          label="Azure project name"
          maxLength={256}
          onChange={(azureProjectName) => setParams({ ...params, azureProjectName })}
          value={params.azureProjectName ?? ""}
        />
      </>
    );
  return null;
}

function paramsForProviderType(
  params: LlmProviderDraft["params"],
  providerType: LlmProviderType,
): LlmProviderDraft["params"] {
  const common = { model: params.model, baseUrl: params.baseUrl };
  if (providerType === "bedrock") return { ...common, awsRegion: params.awsRegion };
  if (providerType === "vertex") {
    return {
      ...common,
      vertexRegion: params.vertexRegion,
      vertexProject: params.vertexProject,
    };
  }
  if (providerType === "azure") {
    return {
      ...common,
      azureResourceName: params.azureResourceName,
      azureResourceType: params.azureResourceType,
      azureApiVersion: params.azureApiVersion,
      azureProjectName: params.azureProjectName,
    };
  }
  return common;
}

function CredentialFields({
  credential,
  configured,
  editing,
  providerType,
  onChange,
}: {
  credential: LlmCredentialInput;
  configured: boolean;
  editing: boolean;
  providerType?: LlmProviderType;
  onChange: (credential: LlmCredentialInput) => void;
}) {
  const { t } = useI18n();
  const options: LlmCredentialInput["mode"][] = editing ? ["preserve", "ambient"] : ["ambient"];
  if (providerType !== "bedrock" && providerType !== "vertex") {
    options.push("environment", "file");
  }
  return (
    <fieldset className="llm-fieldset">
      <legend>{t("Authentication source")}</legend>
      <p>
        {t(
          configured
            ? "Authentication is configured upstream. Its value is not exposed."
            : providerType === "bedrock" || providerType === "vertex"
              ? "Use ambient cloud identity here; structured cloud credentials remain native-console only."
              : "No credential value is exposed by agentgateway.",
        )}
      </p>
      <div className="llm-form-grid">
        <SelectField
          label="Credential mode"
          onChange={(mode) => onChange({ mode: mode as LlmCredentialInput["mode"] })}
          options={options}
          value={credential.mode}
        />
        {credential.mode === "environment" ? (
          <TextField
            label="Environment variable"
            onChange={(reference) => onChange({ ...credential, reference })}
            pattern="[A-Za-z_][A-Za-z0-9_]*"
            placeholder="OPENAI_API_KEY"
            required
            value={credential.reference ?? ""}
          />
        ) : null}
        {credential.mode === "file" ? (
          <TextField
            label="Credential file"
            maxLength={1024}
            onChange={(reference) => onChange({ ...credential, reference })}
            placeholder="/var/run/secrets/provider-key"
            required
            value={credential.reference ?? ""}
          />
        ) : null}
      </div>
    </fieldset>
  );
}

function FormatFields({
  formats,
  onChange,
}: {
  formats: LlmProviderFormat[];
  onChange: (formats: LlmProviderFormat[]) => void;
}) {
  const { t } = useI18n();
  const selected = useMemo(
    () => new Map(formats.map((format) => [format.type, format.path ?? ""])),
    [formats],
  );
  const toggle = (type: LlmProviderFormat["type"], checked: boolean) =>
    onChange(checked ? [...formats, { type }] : formats.filter((format) => format.type !== type));
  const updatePath = (type: LlmProviderFormat["type"], path: string) =>
    onChange(
      formats.map((format) =>
        format.type === type ? { ...format, path: path || undefined } : format,
      ),
    );
  return (
    <fieldset className="llm-fieldset">
      <legend>{t("Custom API formats")}</legend>
      <p>{t("Select at least one format. Paths are optional overrides and must start with /.")}</p>
      <div className="llm-format-list">
        {formatTypes.map((type) => (
          <div className="llm-format-row" key={type}>
            <label>
              <input
                checked={selected.has(type)}
                onChange={(event) => toggle(type, event.target.checked)}
                type="checkbox"
              />{" "}
              {type}
            </label>
            <input
              aria-label={`${type} path`}
              disabled={!selected.has(type)}
              onChange={(event) => updatePath(type, event.target.value)}
              pattern="/.*"
              placeholder={t("Default path")}
              value={selected.get(type) ?? ""}
            />
          </div>
        ))}
      </div>
    </fieldset>
  );
}

function TextField({
  label,
  value,
  onChange,
  placeholder,
  ...props
}: { label: string; value: string; onChange: (value: string) => void } & Omit<
  React.InputHTMLAttributes<HTMLInputElement>,
  "value" | "onChange"
>) {
  const { t } = useI18n();
  return (
    <label className="field llm-field">
      <span>{t(label)}</span>
      <input
        aria-label={t(label)}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder ? t(placeholder) : undefined}
        value={value}
        {...props}
      />
    </label>
  );
}

function SelectField({
  label,
  value,
  options,
  onChange,
  disabled = false,
}: {
  label: string;
  value: string;
  options: string[];
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const { t } = useI18n();
  return (
    <label className="field llm-field">
      <span>{t(label)}</span>
      <select
        aria-label={t(label)}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
        value={value}
      >
        {options.map((option) => (
          <option key={option} value={option}>
            {t(option)}
          </option>
        ))}
      </select>
    </label>
  );
}

function DeleteDialog({
  target,
  confirmed,
  pending,
  error,
  onToggle,
  onClose,
  onConfirm,
}: {
  target?: DeleteTarget;
  confirmed: boolean;
  pending: boolean;
  error?: unknown;
  onToggle: (value: boolean) => void;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const { t } = useI18n();
  const name = target?.item.name ?? "resource";
  return (
    <Dialog
      description="This removes the item from agentgateway configuration after a fresh revision check."
      onClose={onClose}
      open={Boolean(target)}
      title={t("Delete {name}", { name })}
    >
      <div className="dialog-form">
        <p className="llm-delete-copy">
          {t("References are checked again by the server before agentgateway is updated.")}
        </p>
        <label className="confirm-field">
          <input
            checked={confirmed}
            onChange={(event) => onToggle(event.target.checked)}
            type="checkbox"
          />
          {t("I confirm that {name} should be deleted.", { name })}
        </label>
        {error ? <MutationError error={error} /> : null}
        <footer>
          <Button disabled={pending} onClick={onClose} variant="ghost">
            Cancel
          </Button>
          <Button disabled={!confirmed || pending} onClick={onConfirm} variant="danger">
            <Trash2 size={14} /> Delete
          </Button>
        </footer>
      </div>
    </Dialog>
  );
}

function MutationReceipt({ receipt }: { receipt: LlmMutationReceipt }) {
  const { t } = useI18n();
  return (
    <div className="mutation-receipt" role="status">
      <CheckCircle2 size={17} />
      <div>
        <strong>{t(receipt.message)}</strong>
        <span>
          {receipt.target} · {formatTimeWithZone(receipt.completedAt)} · {t("request")}{" "}
          {receipt.requestId}
        </span>
      </div>
    </div>
  );
}

function MutationError({ error }: { error: unknown }) {
  return (
    <div className="protect-error" role="alert">
      {formatError(error)}
    </div>
  );
}
