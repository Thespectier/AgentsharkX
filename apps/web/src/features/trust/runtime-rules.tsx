import { useMutation, useQueryClient } from "@tanstack/react-query";
import { LoaderCircle, ShieldCheck, Sparkles, Trash2 } from "lucide-react";
import { useMemo, useRef, useState } from "react";

import {
  Button,
  Card,
  CardHeader,
  DataTable,
  Dialog,
  EmptyState,
  SourceBadge,
  StatusBadge,
  type Column,
} from "../../components/ui";
import type {
  ProtectMutationReceipt,
  ProtectSnapshot,
  RuntimeRule,
  RuntimeRuleCheck,
} from "../../generated/api-client";
import { mutateOperation } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { synchronizeAgentGuardData } from "../../lib/query-sync";
import { ProtectMutationError, ProtectMutationReceiptNotice } from "../protect/approval-decision";

type RuleRow = RuntimeRule & { type: string };

const defaultRuntimeRuleSource = "RULE: review_external_delivery\nPOLICY: HUMAN_CHECK";

const ruleColumns: Column<RuleRow>[] = [
  {
    key: "rule",
    header: "Rule",
    render: (item) => (
      <div className="primary-cell">
        <ShieldCheck aria-hidden="true" size={15} />
        <span>
          <strong>{item.name}</strong>
          <small>{item.type}</small>
        </span>
      </div>
    ),
  },
  { key: "source", header: "Source", render: (item) => <SourceBadge source={item.source} /> },
  { key: "scope", header: "Scope", render: (item) => item.scope },
  { key: "phase", header: "Phase", render: (item) => <StatusBadge status={item.phase} /> },
  {
    key: "action",
    header: "Action",
    render: (item) => <strong className="decision-text">{item.action}</strong>,
  },
  { key: "status", header: "Status", render: (item) => <StatusBadge status={item.status} /> },
];

export function RuntimeRulesView({ data }: { data: ProtectSnapshot }) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const agents = useMemo(() => {
    const values = new Map<string, string>();
    for (const plugin of data.plugins) values.set(plugin.agentId, plugin.agentUpstreamId);
    for (const rule of data.runtimeRules) {
      if (rule.agentId && rule.agentUpstreamId) values.set(rule.agentId, rule.agentUpstreamId);
    }
    return [...values.entries()];
  }, [data.plugins, data.runtimeRules]);
  const [composerOpen, setComposerOpen] = useState(false);
  const [source, setSource] = useState(defaultRuntimeRuleSource);
  const sourceRef = useRef(defaultRuntimeRuleSource);
  const [agentId, setAgentId] = useState(agents[0]?.[0] ?? "");
  const [note, setNote] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [checkResult, setCheckResult] = useState<RuntimeRuleCheck>();
  const [deleteRule, setDeleteRule] = useState<RuntimeRule>();
  const [deleteNote, setDeleteNote] = useState("");
  const [deleteConfirmed, setDeleteConfirmed] = useState(false);
  const [receipt, setReceipt] = useState<ProtectMutationReceipt>();

  const check = useMutation({
    mutationFn: (candidateSource: string) =>
      mutateOperation("checkRuntimeRule", { source: candidateSource }),
    onSuccess: (response, checkedSource) => {
      if (sourceRef.current === checkedSource) setCheckResult(response.data);
    },
  });
  const publish = useMutation({
    mutationFn: () =>
      mutateOperation(
        "publishRuntimeRule",
        { source, checkToken: checkResult?.checkToken ?? "", note, confirmed },
        { path: { agentId } },
      ),
    onSuccess: (response) => {
      setReceipt(response.data);
      resetComposer();
      void synchronizeAgentGuardData(queryClient);
    },
  });
  const remove = useMutation({
    mutationFn: (rule: RuntimeRule) =>
      mutateOperation(
        "deleteRuntimeRule",
        { note: deleteNote, confirmed: deleteConfirmed },
        { path: { agentId: rule.agentId ?? "", ruleId: rule.id } },
      ),
    onSuccess: (response) => {
      setReceipt(response.data);
      setDeleteRule(undefined);
      setDeleteNote("");
      setDeleteConfirmed(false);
      void synchronizeAgentGuardData(queryClient);
    },
  });

  const rows = data.runtimeRules.map((rule) => ({ ...rule, type: "Runtime Rule" }));
  const columns: Column<RuleRow>[] = [
    ...ruleColumns,
    {
      key: "manage",
      header: "Manage",
      render: (item) => {
        const rule = data.runtimeRules.find((candidate) => candidate.id === item.id)!;
        return rule.userManaged && rule.agentId ? (
          <Button
            aria-label={`${t("Delete")} ${rule.name}`}
            onClick={() => setDeleteRule(rule)}
            size="sm"
            variant="ghost"
          >
            <Trash2 aria-hidden="true" size={13} /> Delete
          </Button>
        ) : (
          <span className="resource-note">{t("Read-only")}</span>
        );
      },
    },
  ];
  const publishReady = Boolean(
    checkResult?.publishable && checkResult.checkToken && agentId && note.trim() && confirmed,
  );

  function resetComposer() {
    setComposerOpen(false);
    setSource(defaultRuntimeRuleSource);
    sourceRef.current = defaultRuntimeRuleSource;
    setAgentId(agents[0]?.[0] ?? "");
    setNote("");
    setConfirmed(false);
    setCheckResult(undefined);
    check.reset();
    publish.reset();
  }

  return (
    <>
      <Card>
        <CardHeader
          action={
            <Button
              disabled={!agents.length}
              onClick={() => {
                resetComposer();
                setComposerOpen(true);
              }}
              variant="primary"
            >
              New rule <Sparkles aria-hidden="true" size={14} />
            </Button>
          }
          description="A successful syntax check creates a short-lived, source-bound, one-use publish token."
          title="Runtime rules"
        />
        {receipt ? <ProtectMutationReceiptNotice receipt={receipt} /> : null}
        {rows.length ? (
          <DataTable columns={columns} data={rows} label="Agentshark runtime rules" />
        ) : (
          <EmptyState description="No runtime rules have been reported." title="No runtime rules" />
        )}
      </Card>

      <Dialog
        description="Check exactly one rule, add an operator note, then explicitly confirm publication. Rule source is never written to audit logs."
        onClose={() => !publish.isPending && resetComposer()}
        open={composerOpen}
        size="wide"
        title="Publish runtime rule"
      >
        <div className="dialog-form protect-form">
          <label className="field">
            <span>{t("Monitored agent")}</span>
            <select
              aria-label={t("Monitored agent")}
              onChange={(event) => setAgentId(event.target.value)}
              value={agentId}
            >
              {agents.map(([id, upstream]) => (
                <option key={id} value={id}>
                  {upstream}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span>{t("Rule source")}</span>
            <textarea
              aria-label={t("Rule source")}
              onChange={(event) => {
                const nextSource = event.target.value;
                sourceRef.current = nextSource;
                setSource(nextSource);
                setCheckResult(undefined);
                check.reset();
                publish.reset();
              }}
              rows={5}
              value={source}
            />
          </label>
          <div className="protect-check-row">
            <Button
              disabled={check.isPending || !source.trim()}
              onClick={() => check.mutate(source)}
            >
              {check.isPending ? (
                <LoaderCircle className="spin" size={14} />
              ) : (
                <ShieldCheck size={14} />
              )}{" "}
              Check syntax
            </Button>
            {checkResult ? (
              <span
                className={
                  checkResult.publishable
                    ? "protect-check protect-check--ok"
                    : "protect-check protect-check--error"
                }
                role="status"
              >
                {checkResult.publishable
                  ? t("Checked and publishable")
                  : (checkResult.errors[0]?.message ?? t("Not publishable"))}
              </span>
            ) : (
              <span className="resource-note">{t("Check required before publish")}</span>
            )}
          </div>
          {check.isError ? <ProtectMutationError error={check.error} /> : null}
          <label className="field">
            <span>{t("Operator note")}</span>
            <textarea
              aria-label={t("Operator note")}
              onChange={(event) => setNote(event.target.value)}
              rows={2}
              value={note}
            />
          </label>
          <label className="confirm-field">
            <input
              checked={confirmed}
              onChange={(event) => setConfirmed(event.target.checked)}
              type="checkbox"
            />
            {t("I confirm this checked rule should be published to the selected agent.")}
          </label>
          {publish.isError ? <ProtectMutationError error={publish.error} /> : null}
          <footer>
            <Button disabled={publish.isPending} onClick={resetComposer} variant="ghost">
              Cancel
            </Button>
            <Button
              disabled={!publishReady || publish.isPending}
              onClick={() => publish.mutate()}
              variant="primary"
            >
              {publish.isPending ? <LoaderCircle className="spin" size={14} /> : null} Publish
              checked rule
            </Button>
          </footer>
        </div>
      </Dialog>

      <Dialog
        description="Deletion is limited to a currently reported user-managed runtime rule."
        onClose={() => !remove.isPending && setDeleteRule(undefined)}
        open={Boolean(deleteRule)}
        title={deleteRule ? `Delete ${deleteRule.name}` : "Delete runtime rule"}
      >
        <div className="dialog-form">
          <label className="field">
            <span>{t("Operator note")}</span>
            <textarea
              aria-label={t("Deletion note")}
              onChange={(event) => setDeleteNote(event.target.value)}
              rows={3}
              value={deleteNote}
            />
          </label>
          <label className="confirm-field">
            <input
              checked={deleteConfirmed}
              onChange={(event) => setDeleteConfirmed(event.target.checked)}
              type="checkbox"
            />
            {t("I confirm this runtime rule should be deleted.")}
          </label>
          {remove.isError ? <ProtectMutationError error={remove.error} /> : null}
          <footer>
            <Button
              disabled={remove.isPending}
              onClick={() => setDeleteRule(undefined)}
              variant="ghost"
            >
              Cancel
            </Button>
            <Button
              disabled={!deleteNote.trim() || !deleteConfirmed || remove.isPending}
              onClick={() => deleteRule && remove.mutate(deleteRule)}
              variant="danger"
            >
              {remove.isPending ? <LoaderCircle className="spin" size={14} /> : null} Delete rule
            </Button>
          </footer>
        </div>
      </Dialog>
    </>
  );
}
