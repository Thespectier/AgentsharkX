import {
  ArrowDown,
  ArrowUp,
  Braces,
  Code2,
  LoaderCircle,
  Plus,
  Save,
  SlidersHorizontal,
  Trash2,
} from "lucide-react";
import { type FormEvent, type ReactNode, useEffect, useState } from "react";

import { Button } from "../../components/ui";
import { ApiError, formatError } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import {
  arrayValue,
  builtinRules,
  changeGuardKind,
  emptyGuardrailValue,
  emptyLlmGuard,
  guardKind,
  isRecord,
  jsonText,
  llmRequestGuardKinds,
  llmResponseGuardKinds,
  mcpPhases,
  moveItem,
  objectValue,
  targetForKind,
  targetKind,
  validateGuardrailValue,
  type GuardrailFamily,
  type GuardrailPhase,
  type JsonRecord,
  type LlmGuardKind,
} from "./gateway-guardrail-model";

type EditorMode = "structured" | "json";

export function GatewayGuardrailEditor({
  family,
  initial,
  pending,
  error,
  onCancel,
  onSave,
}: {
  family: GuardrailFamily;
  initial?: JsonRecord;
  pending: boolean;
  error?: unknown;
  onCancel: () => void;
  onSave: (value: JsonRecord) => void;
}) {
  const { t } = useI18n();
  const [mode, setMode] = useState<EditorMode>("structured");
  const [value, setValue] = useState<JsonRecord>(() =>
    structuredClone(initial ?? emptyGuardrailValue(family)),
  );
  const [rawValue, setRawValue] = useState(() => jsonText(value));
  const [validationError, setValidationError] = useState<string>();
  const updateValue = (next: JsonRecord) => {
    setValue(next);
    setValidationError(undefined);
  };

  const selectMode = (next: EditorMode) => {
    if (next === mode) return;
    if (next === "json") {
      setRawValue(jsonText(value));
      setValidationError(undefined);
      setMode(next);
      return;
    }
    const parsed = parseJsonObject(rawValue);
    if (parsed.error) {
      setValidationError(parsed.error);
      return;
    }
    const candidateError = validateGuardrailValue(family, parsed.value);
    if (candidateError) {
      setValidationError(candidateError);
      return;
    }
    setValue(parsed.value);
    setValidationError(undefined);
    setMode(next);
  };

  const submit = (event: FormEvent) => {
    event.preventDefault();
    let candidate = value;
    if (mode === "json") {
      const parsed = parseJsonObject(rawValue);
      if (parsed.error) {
        setValidationError(parsed.error);
        return;
      }
      candidate = parsed.value;
    }
    const candidateError = validateGuardrailValue(family, candidate);
    if (candidateError) {
      setValidationError(candidateError);
      return;
    }
    setValidationError(undefined);
    onSave(candidate);
  };

  return (
    <form className="dialog-form gateway-guardrail-editor" onSubmit={submit}>
      <div className="gateway-guardrail-editor__mode">
        <div aria-label={t("Guardrail editor mode")} className="mcp-segmented" role="group">
          <button
            aria-pressed={mode === "structured"}
            className={mode === "structured" ? "is-active" : undefined}
            onClick={() => selectMode("structured")}
            type="button"
          >
            <SlidersHorizontal aria-hidden="true" size={14} /> {t("Structured")}
          </button>
          <button
            aria-pressed={mode === "json"}
            className={mode === "json" ? "is-active" : undefined}
            onClick={() => selectMode("json")}
            type="button"
          >
            <Code2 aria-hidden="true" size={14} /> {t("Complete JSON")}
          </button>
        </div>
        <p>
          {mode === "structured"
            ? t("Structured controls preserve advanced source-owned fields that are not changed.")
            : t("Complete JSON exposes every verified agentgateway guardrail field.")}
        </p>
      </div>

      {mode === "structured" ? (
        family === "llm" ? (
          <LlmGuardrailEditor onChange={updateValue} value={value} />
        ) : (
          <McpGuardrailEditor onChange={updateValue} value={value} />
        )
      ) : (
        <label className="llm-field gateway-guardrail-json-field">
          <span>
            {t("Complete guardrail value")} <code>JSON</code>
          </span>
          <textarea
            aria-invalid={Boolean(validationError)}
            autoFocus
            onChange={(event) => {
              setRawValue(event.target.value);
              setValidationError(undefined);
            }}
            rows={24}
            spellCheck={false}
            value={rawValue}
          />
        </label>
      )}

      {validationError ? (
        <p className="mutation-error" role="alert">
          {validationError}
        </p>
      ) : null}
      {error ? <MutationError error={error} /> : null}
      <footer>
        <Button disabled={pending} onClick={onCancel} type="button" variant="ghost">
          Cancel
        </Button>
        <Button disabled={pending} type="submit" variant="primary">
          {pending ? (
            <LoaderCircle aria-hidden="true" className="spin" size={14} />
          ) : (
            <Save aria-hidden="true" size={14} />
          )}
          Save guardrails
        </Button>
      </footer>
    </form>
  );
}

function LlmGuardrailEditor({
  value,
  onChange,
}: {
  value: JsonRecord;
  onChange: (value: JsonRecord) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="gateway-guardrail-structured">
      <fieldset className="gateway-guardrail-fieldset gateway-guardrail-streaming">
        <legend>{t("Streaming guardrails")}</legend>
        <div aria-label={t("Streaming guardrails")} className="mcp-segmented" role="group">
          {(["Disabled", "Enabled"] as const).map((streaming) => (
            <button
              aria-pressed={(value.streaming ?? "Disabled") === streaming}
              className={(value.streaming ?? "Disabled") === streaming ? "is-active" : undefined}
              key={streaming}
              onClick={() => onChange({ ...value, streaming })}
              type="button"
            >
              {t(streaming)}
            </button>
          ))}
        </div>
        <small>
          {t("Applies response guards to streaming responses and realtime WebSocket messages.")}
        </small>
      </fieldset>
      <LlmGuardPhaseEditor
        guards={arrayValue(value.request)}
        onChange={(request) => onChange({ ...value, request })}
        phase="request"
      />
      <LlmGuardPhaseEditor
        guards={arrayValue(value.response)}
        onChange={(response) => onChange({ ...value, response })}
        phase="response"
      />
    </div>
  );
}

function LlmGuardPhaseEditor({
  phase,
  guards,
  onChange,
}: {
  phase: GuardrailPhase;
  guards: unknown[];
  onChange: (guards: unknown[]) => void;
}) {
  const { t } = useI18n();
  const [newKind, setNewKind] = useState<LlmGuardKind>("regex");
  const kinds = phase === "request" ? llmRequestGuardKinds : llmResponseGuardKinds;
  return (
    <fieldset className="gateway-guardrail-fieldset gateway-guardrail-phase-editor">
      <legend>{t(phase === "request" ? "Request guards" : "Response guards")}</legend>
      <p>
        {t(
          phase === "request"
            ? "Inspect prompts before they reach the upstream model."
            : "Inspect model output before it returns to the caller.",
        )}
      </p>
      <div className="gateway-guardrail-editor-list">
        {guards.map((candidate, index) => (
          <LlmGuardCard
            guard={objectValue(candidate)}
            index={index}
            isFirst={index === 0}
            isLast={index === guards.length - 1}
            key={index}
            onChange={(guard) =>
              onChange(
                guards.map((current, currentIndex) => (currentIndex === index ? guard : current)),
              )
            }
            onMove={(direction) => onChange(moveItem(guards, index, direction))}
            onRemove={() => onChange(guards.filter((_, currentIndex) => currentIndex !== index))}
            phase={phase}
          />
        ))}
        {!guards.length ? (
          <p className="gateway-guardrail-empty-inline">{t("No guards configured")}</p>
        ) : null}
      </div>
      <div className="gateway-guardrail-add-row">
        <label className="guardrail-field">
          <span>{t("Guard type")}</span>
          <select
            onChange={(event) => setNewKind(event.target.value as LlmGuardKind)}
            value={newKind}
          >
            {kinds.map((kind) => (
              <option key={kind} value={kind}>
                {t(guardKindLabel(kind))}
              </option>
            ))}
          </select>
        </label>
        <Button onClick={() => onChange([...guards, emptyLlmGuard(newKind)])} type="button">
          <Plus aria-hidden="true" size={14} /> Add guard
        </Button>
      </div>
    </fieldset>
  );
}

function LlmGuardCard({
  phase,
  guard,
  index,
  isFirst,
  isLast,
  onChange,
  onMove,
  onRemove,
}: {
  phase: GuardrailPhase;
  guard: JsonRecord;
  index: number;
  isFirst: boolean;
  isLast: boolean;
  onChange: (guard: JsonRecord) => void;
  onMove: (direction: -1 | 1) => void;
  onRemove: () => void;
}) {
  const { t } = useI18n();
  const currentKind = guardKind(guard);
  const availableKinds = phase === "request" ? llmRequestGuardKinds : llmResponseGuardKinds;
  return (
    <section className="gateway-guardrail-editor-item">
      <header>
        <div>
          <span>{phase === "request" ? t("Request guard") : t("Response guard")}</span>
          <strong>{index + 1}</strong>
        </div>
        <div className="gateway-guardrail-order-actions">
          <IconButton disabled={isFirst} label="Move guard up" onClick={() => onMove(-1)}>
            <ArrowUp aria-hidden="true" size={14} />
          </IconButton>
          <IconButton disabled={isLast} label="Move guard down" onClick={() => onMove(1)}>
            <ArrowDown aria-hidden="true" size={14} />
          </IconButton>
          <IconButton danger label="Remove guard" onClick={onRemove}>
            <Trash2 aria-hidden="true" size={14} />
          </IconButton>
        </div>
      </header>
      {currentKind === "unsupported" ? (
        <div className="gateway-guardrail-advanced-note">
          <Braces aria-hidden="true" size={15} />
          <span>{t("This source guard is preserved. Use Complete JSON to edit its shape.")}</span>
        </div>
      ) : (
        <>
          <Field label="Guard type">
            <select
              onChange={(event) =>
                onChange(changeGuardKind(guard, event.target.value as LlmGuardKind))
              }
              value={currentKind}
            >
              {availableKinds.map((kind) => (
                <option key={kind} value={kind}>
                  {t(guardKindLabel(kind))}
                </option>
              ))}
            </select>
          </Field>
          <LlmVariantFields guard={guard} kind={currentKind} onChange={onChange} phase={phase} />
          <RejectionFields guard={guard} onChange={onChange} />
        </>
      )}
    </section>
  );
}

function LlmVariantFields({
  guard,
  kind,
  phase,
  onChange,
}: {
  guard: JsonRecord;
  kind: LlmGuardKind;
  phase: GuardrailPhase;
  onChange: (guard: JsonRecord) => void;
}) {
  const { t } = useI18n();
  const body = objectValue(guard[kind]);
  const patch = (next: JsonRecord) => onChange({ ...guard, [kind]: { ...body, ...next } });
  if (kind === "regex") return <RegexFields body={body} onChange={patch} />;
  if (kind === "webhook") return <WebhookFields body={body} onChange={patch} />;
  if (kind === "openAIModeration") {
    return (
      <>
        <Field label="Moderation model" hint="Defaults to omni-moderation-latest when empty.">
          <input
            onChange={(event) => patch({ model: event.target.value || undefined })}
            placeholder="omni-moderation-latest"
            value={typeof body.model === "string" ? body.model : ""}
          />
        </Field>
        <AdvancedValueNotice label="Backend policies" value={body.policies} />
      </>
    );
  }
  if (kind === "bedrockGuardrails") {
    return (
      <div className="gateway-guardrail-fields-grid">
        <Field label="Guardrail identifier">
          <input
            onChange={(event) => patch({ guardrailIdentifier: event.target.value })}
            value={stringValue(body.guardrailIdentifier)}
          />
        </Field>
        <Field label="Guardrail version">
          <input
            onChange={(event) => patch({ guardrailVersion: event.target.value })}
            value={stringValue(body.guardrailVersion)}
          />
        </Field>
        <Field label="AWS region">
          <input
            onChange={(event) => patch({ region: event.target.value })}
            placeholder="us-west-2"
            value={stringValue(body.region)}
          />
        </Field>
        <AdvancedValueNotice label="Backend policies" value={body.policies} />
      </div>
    );
  }
  if (kind === "googleModelArmor") {
    return (
      <div className="gateway-guardrail-fields-grid">
        <Field label="Template ID">
          <input
            onChange={(event) => patch({ templateId: event.target.value })}
            value={stringValue(body.templateId)}
          />
        </Field>
        <Field label="Project ID">
          <input
            onChange={(event) => patch({ projectId: event.target.value })}
            value={stringValue(body.projectId)}
          />
        </Field>
        <Field label="Location">
          <input
            onChange={(event) => patch({ location: event.target.value || undefined })}
            placeholder="us-central1"
            value={stringValue(body.location)}
          />
        </Field>
        <AdvancedValueNotice label="Backend policies" value={body.policies} />
      </div>
    );
  }
  const analyze = objectValue(body.analyzeText);
  const jailbreak = objectValue(body.detectJailbreak);
  return (
    <div className="gateway-guardrail-fields-grid">
      <Field label="Azure endpoint" wide>
        <input
          onChange={(event) => patch({ endpoint: event.target.value })}
          placeholder="resource.cognitiveservices.azure.com"
          value={stringValue(body.endpoint)}
        />
      </Field>
      <Field label="Severity threshold" hint="Integer from 0 to 6.">
        <input
          max={6}
          min={0}
          onChange={(event) =>
            patch({
              analyzeText: patchValue(
                analyze,
                "severityThreshold",
                numberOrUndefined(event.target.value),
              ),
            })
          }
          type="number"
          value={numberText(analyze.severityThreshold)}
        />
      </Field>
      <Field label="Analyze API version">
        <input
          onChange={(event) =>
            patch({
              analyzeText: patchValue(analyze, "apiVersion", event.target.value || undefined),
            })
          }
          placeholder="2024-09-01"
          value={stringValue(analyze.apiVersion)}
        />
      </Field>
      <Field label="Blocklist names" hint="Comma-separated values." wide>
        <input
          onChange={(event) =>
            patch({
              analyzeText: patchValue(analyze, "blocklistNames", commaValues(event.target.value)),
            })
          }
          value={stringArray(analyze.blocklistNames).join(", ")}
        />
      </Field>
      <label className="confirm-field gateway-guardrail-checkbox">
        <input
          checked={Boolean(analyze.haltOnBlocklistHit)}
          onChange={(event) =>
            patch({
              analyzeText: patchValue(
                analyze,
                "haltOnBlocklistHit",
                event.target.checked || undefined,
              ),
            })
          }
          type="checkbox"
        />
        {t("Halt on blocklist hit")}
      </label>
      {phase === "request" ? (
        <label className="confirm-field gateway-guardrail-checkbox">
          <input
            checked={isRecord(body.detectJailbreak)}
            onChange={(event) => patch({ detectJailbreak: event.target.checked ? {} : undefined })}
            type="checkbox"
          />
          {t("Detect jailbreak attempts")}
        </label>
      ) : null}
      {phase === "request" && isRecord(body.detectJailbreak) ? (
        <Field label="Jailbreak API version" wide>
          <input
            onChange={(event) =>
              patch({
                detectJailbreak: patchValue(
                  jailbreak,
                  "apiVersion",
                  event.target.value || undefined,
                ),
              })
            }
            placeholder="2024-02-15-preview"
            value={stringValue(jailbreak.apiVersion)}
          />
        </Field>
      ) : null}
      <AdvancedValueNotice label="Backend policies" value={body.policies} />
    </div>
  );
}

function RegexFields({
  body,
  onChange,
}: {
  body: JsonRecord;
  onChange: (value: JsonRecord) => void;
}) {
  const { t } = useI18n();
  const rules = arrayValue(body.rules);
  return (
    <div className="gateway-guardrail-fields-stack">
      <Field label="Match action">
        <select
          onChange={(event) => onChange({ action: event.target.value })}
          value={body.action === "reject" ? "reject" : "mask"}
        >
          <option value="mask">{t("Mask matched text")}</option>
          <option value="reject">{t("Reject traffic")}</option>
        </select>
      </Field>
      <div className="gateway-guardrail-key-values">
        <div className="gateway-guardrail-key-values__label">{t("Regex rules")}</div>
        {rules.map((candidate, index) => {
          const rule = objectValue(candidate);
          const builtin = typeof rule.builtin === "string";
          return (
            <div className="gateway-guardrail-rule-row" key={index}>
              <select
                aria-label={`${t("Rule type")} ${index + 1}`}
                onChange={(event) => {
                  const next =
                    event.target.value === "builtin" ? { builtin: "email" } : { pattern: "" };
                  onChange({
                    rules: rules.map((current, currentIndex) =>
                      currentIndex === index ? next : current,
                    ),
                  });
                }}
                value={builtin ? "builtin" : "pattern"}
              >
                <option value="builtin">{t("Built-in detector")}</option>
                <option value="pattern">{t("Custom regex")}</option>
              </select>
              {builtin ? (
                <select
                  aria-label={`${t("Built-in detector")} ${index + 1}`}
                  onChange={(event) =>
                    onChange({
                      rules: rules.map((current, currentIndex) =>
                        currentIndex === index ? { builtin: event.target.value } : current,
                      ),
                    })
                  }
                  value={stringValue(rule.builtin)}
                >
                  {builtinRules.map((builtinRule) => (
                    <option key={builtinRule} value={builtinRule}>
                      {t(builtinLabel(builtinRule))}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  aria-label={`${t("Custom regex")} ${index + 1}`}
                  className="mono-input"
                  onChange={(event) =>
                    onChange({
                      rules: rules.map((current, currentIndex) =>
                        currentIndex === index ? { pattern: event.target.value } : current,
                      ),
                    })
                  }
                  placeholder="(?i)secret-[a-z0-9]+"
                  value={stringValue(rule.pattern)}
                />
              )}
              <IconButton
                danger
                label="Remove regex rule"
                onClick={() =>
                  onChange({ rules: rules.filter((_, currentIndex) => currentIndex !== index) })
                }
              >
                <Trash2 aria-hidden="true" size={14} />
              </IconButton>
            </div>
          );
        })}
        <Button
          onClick={() => onChange({ rules: [...rules, { pattern: "" }] })}
          size="sm"
          type="button"
        >
          <Plus aria-hidden="true" size={14} /> Add regex rule
        </Button>
      </div>
    </div>
  );
}

function WebhookFields({
  body,
  onChange,
}: {
  body: JsonRecord;
  onChange: (value: JsonRecord) => void;
}) {
  const { t } = useI18n();
  const target = objectValue(body.target);
  const kind = targetKind(target);
  const service = objectValue(target.service);
  return (
    <div className="gateway-guardrail-fields-grid">
      <Field label="Target type">
        <select
          onChange={(event) =>
            onChange({
              target: targetForKind(event.target.value as ReturnType<typeof targetKind>, target),
            })
          }
          value={kind}
        >
          <option value="host">{t("Host")}</option>
          <option value="backend">{t("Backend reference")}</option>
          <option value="service">{t("Service reference")}</option>
        </select>
      </Field>
      {kind === "host" ? (
        <Field label="Webhook host">
          <input
            onChange={(event) => onChange({ target: { host: event.target.value } })}
            placeholder="guardrail.internal:8080"
            value={stringValue(target.host)}
          />
        </Field>
      ) : null}
      {kind === "backend" ? (
        <Field label="Backend reference">
          <input
            onChange={(event) => onChange({ target: { backend: event.target.value } })}
            value={stringValue(target.backend)}
          />
        </Field>
      ) : null}
      {kind === "service" ? (
        <>
          <Field label="Service name">
            <input
              onChange={(event) =>
                onChange({ target: { service: { ...service, name: event.target.value } } })
              }
              placeholder="default/guardrail"
              value={stringValue(service.name)}
            />
          </Field>
          <Field label="Service port">
            <input
              max={65535}
              min={0}
              onChange={(event) =>
                onChange({ target: { service: { ...service, port: Number(event.target.value) } } })
              }
              type="number"
              value={numberText(service.port)}
            />
          </Field>
        </>
      ) : null}
      <Field label="Failure mode">
        <select
          onChange={(event) => onChange({ failureMode: event.target.value })}
          value={body.failureMode === "failOpen" ? "failOpen" : "failClosed"}
        >
          <option value="failClosed">{t("Fail closed")}</option>
          <option value="failOpen">{t("Fail open")}</option>
        </select>
      </Field>
      <AdvancedValueNotice label="Forwarded header matches" value={body.forwardHeaderMatches} />
    </div>
  );
}

function RejectionFields({
  guard,
  onChange,
}: {
  guard: JsonRecord;
  onChange: (guard: JsonRecord) => void;
}) {
  const { t } = useI18n();
  const rejection = objectValue(guard.rejection);
  const patch = (key: string, value: unknown) => {
    const next = patchValue(rejection, key, value);
    onChange({ ...guard, rejection: Object.keys(next).length ? next : undefined });
  };
  return (
    <details className="gateway-guardrail-rejection">
      <summary>{t("Rejection response")}</summary>
      <div className="gateway-guardrail-fields-grid">
        <Field label="HTTP status">
          <input
            max={599}
            min={100}
            onChange={(event) => patch("status", numberOrUndefined(event.target.value))}
            placeholder="403"
            type="number"
            value={numberText(rejection.status)}
          />
        </Field>
        <Field label="Response body" wide>
          <textarea
            onChange={(event) => patch("body", event.target.value || undefined)}
            rows={3}
            value={stringValue(rejection.body)}
          />
        </Field>
        <AdvancedValueNotice label="Response headers" value={rejection.headers} />
      </div>
    </details>
  );
}

function McpGuardrailEditor({
  value,
  onChange,
}: {
  value: JsonRecord;
  onChange: (value: JsonRecord) => void;
}) {
  const { t } = useI18n();
  const processors = arrayValue(value.processors);
  const updateProcessors = (next: unknown[]) => onChange({ ...value, processors: next });
  return (
    <div className="gateway-guardrail-structured">
      <div className="gateway-guardrail-section-heading">
        <div>
          <strong>{t("Ordered processors")}</strong>
          <p>{t("Processors run in order; the first rejection stops the chain.")}</p>
        </div>
        <Button
          onClick={() =>
            updateProcessors([
              ...processors,
              {
                kind: "remote",
                host: "",
                failureMode: "failClosed",
                methods: { "tools/call": "request" },
              },
            ])
          }
          size="sm"
          type="button"
        >
          <Plus aria-hidden="true" size={14} /> Add processor
        </Button>
      </div>
      <div className="gateway-guardrail-editor-list">
        {processors.map((candidate, index) => (
          <McpProcessorCard
            index={index}
            isFirst={index === 0}
            isLast={index === processors.length - 1}
            key={index}
            onChange={(processor) =>
              updateProcessors(
                processors.map((current, currentIndex) =>
                  currentIndex === index ? processor : current,
                ),
              )
            }
            onMove={(direction) => updateProcessors(moveItem(processors, index, direction))}
            onRemove={() =>
              updateProcessors(processors.filter((_, currentIndex) => currentIndex !== index))
            }
            processor={objectValue(candidate)}
          />
        ))}
        {!processors.length ? (
          <p className="gateway-guardrail-empty-inline">{t("No processors configured")}</p>
        ) : null}
      </div>
    </div>
  );
}

function McpProcessorCard({
  processor,
  index,
  isFirst,
  isLast,
  onChange,
  onMove,
  onRemove,
}: {
  processor: JsonRecord;
  index: number;
  isFirst: boolean;
  isLast: boolean;
  onChange: (processor: JsonRecord) => void;
  onMove: (direction: -1 | 1) => void;
  onRemove: () => void;
}) {
  const { t } = useI18n();
  const kind = targetKind(processor);
  const service = objectValue(processor.service);
  const methods = objectValue(processor.methods);
  const metadata = objectValue(processor.metadata);
  const requestHeaders = objectValue(processor.requestHeaders);
  const setTarget = (target: JsonRecord) => {
    const next = { ...processor };
    delete next.host;
    delete next.backend;
    delete next.service;
    onChange({ ...next, ...target });
  };
  return (
    <section className="gateway-guardrail-editor-item gateway-guardrail-processor-card">
      <header>
        <div>
          <span>{t("Remote processor")}</span>
          <strong>{index + 1}</strong>
        </div>
        <div className="gateway-guardrail-order-actions">
          <IconButton disabled={isFirst} label="Move processor up" onClick={() => onMove(-1)}>
            <ArrowUp aria-hidden="true" size={14} />
          </IconButton>
          <IconButton disabled={isLast} label="Move processor down" onClick={() => onMove(1)}>
            <ArrowDown aria-hidden="true" size={14} />
          </IconButton>
          <IconButton danger label="Remove processor" onClick={onRemove}>
            <Trash2 aria-hidden="true" size={14} />
          </IconButton>
        </div>
      </header>
      <div className="gateway-guardrail-fields-grid">
        <Field label="Processor kind">
          <input disabled value="remote" />
        </Field>
        <Field label="Target type">
          <select
            onChange={(event) =>
              setTarget(
                targetForKind(event.target.value as ReturnType<typeof targetKind>, processor),
              )
            }
            value={kind}
          >
            <option value="host">{t("Host")}</option>
            <option value="backend">{t("Backend reference")}</option>
            <option value="service">{t("Service reference")}</option>
          </select>
        </Field>
        {kind === "host" ? (
          <Field label="Processor host">
            <input
              onChange={(event) => setTarget({ host: event.target.value })}
              placeholder="guardrail.example.com:9000"
              value={stringValue(processor.host)}
            />
          </Field>
        ) : null}
        {kind === "backend" ? (
          <Field label="Backend reference">
            <input
              onChange={(event) => setTarget({ backend: event.target.value })}
              value={stringValue(processor.backend)}
            />
          </Field>
        ) : null}
        {kind === "service" ? (
          <>
            <Field label="Service name">
              <input
                onChange={(event) =>
                  setTarget({ service: { ...service, name: event.target.value } })
                }
                placeholder="default/guardrail"
                value={stringValue(service.name)}
              />
            </Field>
            <Field label="Service port">
              <input
                max={65535}
                min={0}
                onChange={(event) =>
                  setTarget({ service: { ...service, port: Number(event.target.value) } })
                }
                type="number"
                value={numberText(service.port)}
              />
            </Field>
          </>
        ) : null}
        <Field label="Failure mode">
          <select
            onChange={(event) => onChange({ ...processor, failureMode: event.target.value })}
            value={processor.failureMode === "failOpen" ? "failOpen" : "failClosed"}
          >
            <option value="failClosed">{t("Fail closed")}</option>
            <option value="failOpen">{t("Fail open")}</option>
          </select>
        </Field>
      </div>
      <KeyValueEditor
        addLabel="Add method match"
        entries={methods}
        label="Method phases"
        onChange={(next) => onChange({ ...processor, methods: next })}
        valueOptions={mcpPhases}
      />
      <KeyValueEditor
        addLabel="Add metadata"
        entries={metadata}
        label="CEL metadata"
        onChange={(next) => onChange({ ...processor, metadata: next })}
      />
      <div className="gateway-guardrail-fields-grid">
        <Field
          label="Allowed request headers"
          hint="Comma-separated. Empty forwards all except disallowed headers."
        >
          <input
            aria-label={t("Allowed request headers")}
            onChange={(event) =>
              onChange({
                ...processor,
                requestHeaders: { ...requestHeaders, allowed: commaValues(event.target.value) },
              })
            }
            value={stringArray(requestHeaders.allowed).join(", ")}
          />
        </Field>
        <Field label="Disallowed request headers" hint="Comma-separated. Disallowed always wins.">
          <input
            aria-label={t("Disallowed request headers")}
            onChange={(event) =>
              onChange({
                ...processor,
                requestHeaders: { ...requestHeaders, disallowed: commaValues(event.target.value) },
              })
            }
            value={stringArray(requestHeaders.disallowed).join(", ")}
          />
        </Field>
        <AdvancedValueNotice label="Backend policies" value={processor.policies} />
      </div>
    </section>
  );
}

function KeyValueEditor({
  label,
  entries,
  valueOptions,
  addLabel,
  onChange,
}: {
  label: string;
  entries: JsonRecord;
  valueOptions?: readonly string[];
  addLabel: string;
  onChange: (entries: JsonRecord) => void;
}) {
  const { t } = useI18n();
  const rows = Object.entries(entries);
  const [collision, setCollision] = useState<string>();
  const [keyDrafts, setKeyDrafts] = useState(() => rows.map(([key]) => key));
  useEffect(() => {
    setKeyDrafts(rows.map(([key]) => key));
  }, [entries]);
  const rename = (index: number, key: string, value: unknown) => {
    const next: JsonRecord = {};
    rows.forEach(([currentKey, currentValue], currentIndex) => {
      next[currentIndex === index ? key : currentKey] =
        currentIndex === index ? value : currentValue;
    });
    onChange(next);
  };
  const commitKey = (index: number) => {
    const previousKey = rows[index]?.[0] ?? "";
    const key = keyDrafts[index] ?? previousKey;
    if (key !== previousKey && Object.hasOwn(entries, key)) {
      setCollision(key);
      setKeyDrafts(rows.map(([rowKey]) => rowKey));
      return;
    }
    setCollision(undefined);
    rename(index, key, rows[index]?.[1]);
  };
  return (
    <div className="gateway-guardrail-key-values">
      <div className="gateway-guardrail-key-values__label">{t(label)}</div>
      {rows.map(([key, value], index) => (
        <div className="gateway-guardrail-key-value-row" key={`${key}:${index}`}>
          <input
            aria-label={`${t(label)} ${t("key")} ${index + 1}`}
            className="mono-input"
            onBlur={() => commitKey(index)}
            onChange={(event) =>
              setKeyDrafts((current) =>
                current.map((draft, currentIndex) =>
                  currentIndex === index ? event.target.value : draft,
                ),
              )
            }
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                event.currentTarget.blur();
              }
            }}
            value={keyDrafts[index] ?? key}
          />
          {valueOptions ? (
            <select
              aria-label={`${t(label)} ${t("value")} ${index + 1}`}
              onChange={(event) => rename(index, key, event.target.value)}
              value={String(value)}
            >
              {valueOptions.map((option) => (
                <option key={option} value={option}>
                  {t(option)}
                </option>
              ))}
            </select>
          ) : (
            <input
              aria-label={`${t(label)} ${t("value")} ${index + 1}`}
              className="mono-input"
              onChange={(event) => rename(index, key, event.target.value)}
              value={String(value)}
            />
          )}
          <IconButton
            danger
            label={`Remove ${label.toLowerCase()} entry`}
            onClick={() => {
              setCollision(undefined);
              onChange(
                Object.fromEntries(rows.filter((_, currentIndex) => currentIndex !== index)),
              );
            }}
          >
            <Trash2 aria-hidden="true" size={14} />
          </IconButton>
        </div>
      ))}
      {collision ? (
        <p className="mutation-error" role="alert">
          {t("Keys must be unique.")} <code>{collision}</code>
        </p>
      ) : null}
      <Button
        onClick={() => {
          setCollision(undefined);
          const prefix = valueOptions ? "method" : "metadata";
          let key = valueOptions ? "tools/call" : "key";
          let suffix = 2;
          while (Object.hasOwn(entries, key)) key = `${prefix}-${suffix++}`;
          onChange({ ...entries, [key]: valueOptions ? "request" : "" });
        }}
        size="sm"
        type="button"
      >
        <Plus aria-hidden="true" size={14} /> {addLabel}
      </Button>
    </div>
  );
}

function Field({
  label,
  hint,
  wide,
  children,
}: {
  label: string;
  hint?: string;
  wide?: boolean;
  children: ReactNode;
}) {
  const { t } = useI18n();
  return (
    <label className={`guardrail-field${wide ? " guardrail-field--wide" : ""}`}>
      <span>{t(label)}</span>
      {children}
      {hint ? <small>{t(hint)}</small> : null}
    </label>
  );
}

function AdvancedValueNotice({ label, value }: { label: string; value: unknown }) {
  const { t } = useI18n();
  if (value === undefined || value === null) return null;
  return (
    <details className="gateway-guardrail-advanced-value">
      <summary>
        <Braces aria-hidden="true" size={14} /> {t(label)} · {t("preserved")}
      </summary>
      <pre>{jsonText(value)}</pre>
      <small>{t("Edit this field in Complete JSON mode.")}</small>
    </details>
  );
}

function IconButton({
  label,
  danger,
  disabled,
  onClick,
  children,
}: {
  label: string;
  danger?: boolean;
  disabled?: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  const { t } = useI18n();
  return (
    <Button
      aria-label={t(label)}
      disabled={disabled}
      onClick={onClick}
      size="sm"
      title={t(label)}
      type="button"
      variant={danger ? "danger" : "ghost"}
    >
      {children}
    </Button>
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

function parseJsonObject(
  value: string,
): { value: JsonRecord; error?: undefined } | { value: JsonRecord; error: string } {
  try {
    const parsed = JSON.parse(value) as unknown;
    if (!isRecord(parsed)) return { value: {}, error: "Guardrail value must be a JSON object." };
    return { value: parsed };
  } catch (error) {
    return { value: {}, error: error instanceof Error ? error.message : "Invalid JSON value." };
  }
}

function patchValue(record: JsonRecord, key: string, value: unknown) {
  const next = { ...record };
  if (value === undefined) delete next[key];
  else next[key] = value;
  return next;
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value : "";
}

function numberText(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) ? String(value) : "";
}

function numberOrUndefined(value: string) {
  return value === "" ? undefined : Number(value);
}

function commaValues(value: string) {
  return value
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function stringArray(value: unknown) {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : [];
}

function guardKindLabel(kind: LlmGuardKind) {
  const labels: Record<LlmGuardKind, string> = {
    regex: "Regex rules",
    webhook: "Webhook",
    openAIModeration: "OpenAI Moderation",
    bedrockGuardrails: "Bedrock Guardrails",
    googleModelArmor: "Google Model Armor",
    azureContentSafety: "Azure Content Safety",
  };
  return labels[kind];
}

function builtinLabel(value: (typeof builtinRules)[number]) {
  const labels = {
    ssn: "US Social Security number",
    creditCard: "Credit card",
    phoneNumber: "Phone number",
    email: "Email address",
    caSin: "Canadian SIN",
  };
  return labels[value];
}
