# AgentsharkX web console

The console supports two explicit modes. Mock mode is enabled by default for
deterministic visual review. Setting `VITE_ENABLE_MOCKS=false` uses the Phase 2
Go BFF, including the one-time admin-token exchange and live health/capability
responses. Neither mode gives the browser an upstream credential.

## Commands

```bash
npm ci
npm run dev
npm run api:generate
npm run api:check
npm run check
npm run playwright:install
npm run test:e2e
npm run test:a11y
npm run test:visual:update
npm run lighthouse
```

The host needs Chromium's native libraries. Where those packages are not
available, use the Playwright image matching `package-lock.json`:

```bash
docker run --rm --ipc=host --network=host --user "$(id -u):$(id -g)" \
  -v "$(git rev-parse --show-toplevel):/work" -w /work/apps/web \
  mcr.microsoft.com/playwright:v1.61.1-noble npm run test:e2e

docker run --rm --ipc=host --network=host --user "$(id -u):$(id -g)" \
  -v "$(git rev-parse --show-toplevel):/work" -w /work/apps/web \
  mcr.microsoft.com/playwright:v1.61.1-noble npm run lighthouse
```

The demo-state selector in the top bar exposes live mock, empty, loading,
partial-failure, and total-failure states. `?scenario=...` keeps each state
shareable and deterministic.

The UI treats source ownership as data: every normalized object retains its
`source`; event records also retain a Mock source ID and a redacted raw
reference. MSW is enabled by default only for this Phase 1 preview and can be
disabled with `VITE_ENABLE_MOCKS=false`. Vite proxies `/api` to
`VITE_BFF_PROXY_TARGET` (default `http://127.0.0.1:8080`) in development.
`src/generated/api-client.ts` is
deterministically generated from `api/openapi.yaml`; `npm run check` fails when
the generated client is stale.
