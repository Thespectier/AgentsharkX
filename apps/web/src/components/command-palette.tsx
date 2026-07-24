import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import {
  Activity,
  Cable,
  Command,
  Home,
  Search,
  Settings,
  ShieldCheck,
  Sparkles,
  UserRoundCheck,
  X,
} from "lucide-react";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useMemo, useRef, useState } from "react";

import { Button, cn } from "./ui";
import { useI18n } from "../lib/i18n";

const commands = [
  { label: "Open Home", hint: "Runtime posture", path: "/", icon: Home },
  { label: "Open Connect", hint: "Gateway resources", path: "/connect/overview", icon: Cable },
  {
    label: "Open Trust",
    hint: "Agents and resources",
    path: "/trust/agents",
    icon: UserRoundCheck,
  },
  {
    label: "Open Protect",
    hint: "Policies and approvals",
    path: "/protect/policies",
    icon: ShieldCheck,
  },
  {
    label: "Open Audit",
    hint: "Traffic and security events",
    path: "/audit/analytics",
    icon: Activity,
  },
  { label: "Open System", hint: "Capabilities and sources", path: "/system", icon: Settings },
  {
    label: "Show pending approvals",
    hint: "3 need review",
    path: "/protect/approvals",
    icon: Sparkles,
  },
];

export function CommandPalette({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useI18n();
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const navigate = useNavigate();
  const reduced = useReducedMotion();
  const filtered = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    if (!normalized) return commands;
    return commands.filter((item) =>
      `${item.label} ${item.hint} ${t(item.label)} ${t(item.hint)}`
        .toLowerCase()
        .includes(normalized),
    );
  }, [query, t]);

  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        onOpenChange(!open);
      }
      if (event.key === "Escape" && open) onOpenChange(false);
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [onOpenChange, open]);

  useEffect(() => {
    if (!open) return;
    setQuery("");
    setActive(0);
    requestAnimationFrame(() => inputRef.current?.focus());
  }, [open]);

  const run = (path: string) => {
    onOpenChange(false);
    const section = path.split("/")[2];
    if (path === "/") void navigate({ to: "/", search: true });
    else if (path === "/system") void navigate({ to: "/system", search: true });
    else if (path.startsWith("/connect/"))
      void navigate({ to: "/connect/$section", params: { section }, search: true });
    else if (path.startsWith("/trust/"))
      void navigate({ to: "/trust/$section", params: { section }, search: true });
    else if (path.startsWith("/protect/"))
      void navigate({ to: "/protect/$section", params: { section }, search: true });
    else if (path.startsWith("/audit/"))
      void navigate({ to: "/audit/$section", params: { section }, search: true });
  };

  return (
    <AnimatePresence>
      {open ? (
        <div className="command-layer">
          <motion.button
            aria-label={t("Close command palette")}
            className="command-backdrop"
            onClick={() => onOpenChange(false)}
          />
          <motion.div
            aria-label={t("Command palette")}
            aria-modal="true"
            className="command-palette"
            initial={reduced ? false : { opacity: 0, y: -12, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: -8, scale: 0.99 }}
            role="dialog"
          >
            <div className="command-search">
              <Search aria-hidden="true" size={18} />
              <input
                aria-activedescendant={filtered[active] ? `command-${active}` : undefined}
                aria-controls="command-results"
                aria-label={t("Search commands")}
                onChange={(event) => {
                  setQuery(event.target.value);
                  setActive(0);
                }}
                onKeyDown={(event) => {
                  if (event.key === "ArrowDown") {
                    event.preventDefault();
                    setActive((value) => Math.min(value + 1, filtered.length - 1));
                  }
                  if (event.key === "ArrowUp") {
                    event.preventDefault();
                    setActive((value) => Math.max(value - 1, 0));
                  }
                  if (event.key === "Enter" && filtered[active]) run(filtered[active].path);
                }}
                placeholder={t("Jump to a workspace or action…")}
                ref={inputRef}
                role="combobox"
                value={query}
              />
              <Button
                aria-label={t("Close command palette")}
                onClick={() => onOpenChange(false)}
                size="sm"
                variant="ghost"
              >
                <X size={17} />
              </Button>
            </div>
            <div className="command-results" id="command-results" role="listbox">
              {filtered.length ? (
                filtered.map((item, index) => {
                  const Icon = item.icon;
                  return (
                    <button
                      aria-selected={active === index}
                      className={cn("command-result", active === index && "command-result--active")}
                      id={`command-${index}`}
                      key={item.label}
                      onClick={() => run(item.path)}
                      onMouseEnter={() => setActive(index)}
                      role="option"
                    >
                      <span className="command-result__icon">
                        <Icon aria-hidden="true" size={17} />
                      </span>
                      <span>
                        <strong>{t(item.label)}</strong>
                        <small>{t(item.hint)}</small>
                      </span>
                      <kbd>↵</kbd>
                    </button>
                  );
                })
              ) : (
                <div className="command-empty">
                  <Command size={22} />
                  <p>{t("No matching command")}</p>
                </div>
              )}
            </div>
            <footer className="command-footer">
              <span>
                <kbd>↑</kbd>
                <kbd>↓</kbd> {t("Navigate")}
              </span>
              <span>
                <kbd>esc</kbd> {t("Close")}
              </span>
              <span>{t("Mock console")}</span>
            </footer>
          </motion.div>
        </div>
      ) : null}
    </AnimatePresence>
  );
}
