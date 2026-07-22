import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "../mocks/server";
import { AuthGate } from "./auth-gate";

describe("admin session gate", () => {
  it("exchanges the token without persisting it in browser storage", async () => {
    let authenticated = false;
    server.use(
      http.get("/api/v1/auth/session", () => {
        if (!authenticated) {
          return HttpResponse.json(
            {
              error: {
                code: "AUTH_REQUIRED",
                message: "an authenticated admin session is required",
                requestId: "req-auth-test",
                retryable: false,
              },
            },
            { status: 401 },
          );
        }
        return new HttpResponse(null, {
          status: 204,
          headers: { "X-CSRF-Token": "csrf-test" },
        });
      }),
      http.post("/api/v1/auth/session", async ({ request }) => {
        const body = (await request.json()) as { token?: string };
        if (body.token !== "temporary-admin-token") {
          return HttpResponse.json(
            {
              error: {
                code: "INVALID_CREDENTIALS",
                message: "invalid credentials",
                requestId: "req-login-test",
                retryable: false,
              },
            },
            { status: 401 },
          );
        }
        authenticated = true;
        return new HttpResponse(null, { status: 204, headers: { "X-CSRF-Token": "csrf-test" } });
      }),
    );

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={client}>
        <AuthGate enabled>
          <p>Authenticated console</p>
        </AuthGate>
      </QueryClientProvider>,
    );

    const input = await screen.findByLabelText("Administrator token");
    await user.type(input, "temporary-admin-token");
    await user.click(screen.getByRole("button", { name: "Continue securely" }));

    expect(await screen.findByText("Authenticated console")).toBeVisible();
    expect(localStorage.getItem("temporary-admin-token")).toBeNull();
    expect(Object.values(localStorage)).not.toContain("temporary-admin-token");
  });
});
