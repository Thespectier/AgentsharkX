# Screenshot baselines

Phase 1 stores deterministic Playwright baselines here:

- [`home-1440.png`](home-1440.png): 1440 × 1000 desktop Home with LiveFlow,
  metrics, chart, and source-distinct activity.
- [`audit-1280.png`](audit-1280.png): 1280 × 800 laptop Audit security-event
  table with explicit source and correlation state.
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

All displayed business data is explicitly labelled Mock. The screenshots are
visual evidence of the Phase 1 frontend, not evidence of a live upstream
connection.
