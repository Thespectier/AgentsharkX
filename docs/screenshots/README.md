# Screenshot baselines

Deterministic Playwright release and feature baselines are stored here:

- [`home-1440.png`](home-1440.png): 1440 × 1000 desktop Home with the Agent
  monitoring overview, Beijing-time greeting, live metrics, security queue, and
  source-distinct activity.
- [`audit-1280.png`](audit-1280.png): 1280 × 800 laptop Agent Trace list, the
  default Audit destination, with filters and explicit trace evidence.
- [`connect-1280.png`](connect-1280.png): 1280 × 900 Connect overview with all
  five current section links in the sidebar.
- [`agents-mobile-390.png`](agents-mobile-390.png): 390 × 844 Connect Agent
  monitoring overview with responsive metrics and status rows.
- [`trust-1280.png`](trust-1280.png): 1280 × 900 Trust overview with configured
  policy and published runtime-rule status.
- [`protect-1280.png`](protect-1280.png): 1280 × 900 Protect overview with the
  four explicit outcome categories and pending-approval sidebar badge.
- [`guardrails-1280.png`](guardrails-1280.png): 1280 × 900 Trust workspace
  with complete LLM guardrail placement, phases, ordered guards, and source path.
- [`runtime-rule-dialog-800.png`](runtime-rule-dialog-800.png): 800 × 700
  bounded runtime-rule composer with its validation and confirmation workflow.
- [`lighthouse-accessibility.json`](lighthouse-accessibility.json): Lighthouse
  13.4.1 accessibility result; the committed run scored 100/100.

`apps/web/tests/console.spec.ts` covers Home plus all four workspaces, immediate
sidebar section navigation, mobile behavior after a stored desktop collapse,
the persisted English/Chinese switch, Beijing-time greeting and timestamp
presentation, the four required empty/loading/partial/error states, URL-restored
detail, keyboard command navigation, and reduced motion.
`accessibility.spec.ts` runs Axe across the five primary pages. Regenerate and
compare baselines with:

```bash
npm --prefix apps/web run test:visual:update
npm --prefix apps/web run test:e2e
npm --prefix apps/web run lighthouse
```

All displayed business data is explicitly labelled Mock. Screenshots are
deterministic visual evidence, while `make release-e2e` separately proves the
real BFF login, connection, event, Audit, and approval path.
