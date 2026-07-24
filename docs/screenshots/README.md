# Screenshot baselines

Phase 7 stores deterministic Playwright release baselines here:

- [`home-1440.png`](home-1440.png): 1440 × 1000 desktop Home with LiveFlow,
  metrics, chart, and source-distinct activity.
- [`audit-1280.png`](audit-1280.png): 1280 × 800 laptop Audit analytics with
  exact rolling-window traffic and latency charts.
- [`connect-1280.png`](connect-1280.png): 1280 × 900 Connect overview.
- [`trust-1280.png`](trust-1280.png): 1280 × 900 Trust agent inventory.
- [`protect-1280.png`](protect-1280.png): 1280 × 900 source-separated Protect
  policies.
- [`system-degraded-1440.png`](system-degraded-1440.png): full-page 1440 px
  System view with AgentGuard disconnected and actionable recovery checks.
- [`lighthouse-accessibility.json`](lighthouse-accessibility.json): Lighthouse
  13.4.1 accessibility result; the committed run scored 100/100.

`apps/web/tests/console.spec.ts` covers Home plus all four workspaces, the four
required empty/loading/partial/error states, URL-restored detail, keyboard
command navigation, and reduced motion. `accessibility.spec.ts` runs Axe across
the five primary pages. Regenerate and compare baselines with:

```bash
npm --prefix apps/web run test:visual:update
npm --prefix apps/web run test:e2e
npm --prefix apps/web run lighthouse
```

All displayed business data is explicitly labelled Mock. Screenshots are
deterministic visual evidence, while `make release-e2e` separately proves the
real BFF login, connection, event, Audit, and approval path.
