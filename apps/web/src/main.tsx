import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { router } from "./app/router";
import { AuthGate } from "./app/auth-gate";
import "./styles/index.css";

async function enableMocks() {
  if (import.meta.env.VITE_ENABLE_MOCKS === "false") return;
  const { worker } = await import("./mocks/browser");
  await worker.start({
    onUnhandledRequest: "bypass",
    quiet: true,
    serviceWorker: { url: "/mockServiceWorker.js" },
  });
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
      staleTime: 10_000,
    },
  },
});

function syncDocumentVisibility() {
  document.documentElement.dataset.visibility = document.visibilityState;
}

syncDocumentVisibility();
document.addEventListener("visibilitychange", syncDocumentVisibility);

await enableMocks();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <AuthGate>
        <RouterProvider router={router} />
      </AuthGate>
    </QueryClientProvider>
  </StrictMode>,
);
