import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LockKeyhole, Radar, ShieldCheck } from "lucide-react";
import { type FormEvent, type ReactNode, useEffect, useState } from "react";

import { Button, ErrorState } from "../components/ui";
import {
  ApiError,
  createAdminSession,
  formatError,
  isMockMode,
  requestOperation,
} from "../lib/api";

export function AuthGate({
  children,
  enabled = !isMockMode(),
}: {
  children: ReactNode;
  enabled?: boolean;
}) {
  const queryClient = useQueryClient();
  const [token, setToken] = useState("");
  const [sessionLost, setSessionLost] = useState(false);
  const session = useQuery({
    queryKey: ["admin-session"],
    queryFn: async ({ signal }) => {
      await requestOperation("getAdminSession", signal);
      return true;
    },
    enabled,
    refetchInterval: false,
    refetchOnWindowFocus: false,
    retry: false,
    staleTime: Infinity,
  });
  const login = useMutation({
    mutationFn: createAdminSession,
    onSuccess: async () => {
      setToken("");
      setSessionLost(false);
      await queryClient.resetQueries();
    },
  });

  useEffect(() => {
    if (!enabled) return;
    const handleUnauthorized = () => setSessionLost(true);
    window.addEventListener("agentshark:unauthorized", handleUnauthorized);
    return () => window.removeEventListener("agentshark:unauthorized", handleUnauthorized);
  }, [enabled]);

  if (!enabled) return children;
  const unauthorized =
    sessionLost || (session.error instanceof ApiError && session.error.status === 401);
  if (session.isPending && !unauthorized) {
    return (
      <div className="auth-screen" role="status">
        <div className="auth-probe">
          <Radar aria-hidden="true" size={22} />
          <span>Checking the control-plane session…</span>
        </div>
      </div>
    );
  }
  if (session.isError && !unauthorized) {
    return (
      <div className="auth-screen">
        <div className="auth-card">
          <ErrorState
            description={formatError(session.error)}
            onRetry={() => void session.refetch()}
          />
        </div>
      </div>
    );
  }
  if (unauthorized) {
    const submit = (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      if (token) login.mutate({ token });
    };
    return (
      <main className="auth-screen">
        <section aria-labelledby="auth-title" className="auth-card">
          <div className="auth-card__mark">
            <ShieldCheck aria-hidden="true" size={25} />
          </div>
          <p className="eyebrow">AgentsharkX / Admin session</p>
          <h1 id="auth-title">Unlock the control plane</h1>
          <p>
            The token is exchanged once for a strict browser session. It is never stored in local
            storage or exposed to upstream services.
          </p>
          <form onSubmit={submit}>
            <label htmlFor="admin-token">Administrator token</label>
            <div className="auth-field">
              <LockKeyhole aria-hidden="true" size={17} />
              <input
                autoComplete="current-password"
                autoFocus
                id="admin-token"
                onChange={(event) => setToken(event.target.value)}
                type="password"
                value={token}
              />
            </div>
            {login.isError ? (
              <p className="auth-error" role="alert">
                {formatError(login.error)}
              </p>
            ) : null}
            <Button disabled={!token || login.isPending} type="submit">
              {login.isPending ? "Creating session…" : "Continue securely"}
            </Button>
          </form>
        </section>
      </main>
    );
  }
  return children;
}
