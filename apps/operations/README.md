# The Social Wire Operations

Dedicated operator console for ingestion health, AppView observability, gaps, backfills, alerts, runbooks, and trace inspection.

## Local Development

```sh
bun --cwd apps/operations dev
```

Set `NEXT_PUBLIC_OPERATIONS_DEMO_MODE=1` for the explicit local demo dataset. Normal operation uses ATProto browser OAuth and the Gateway origins below.

## Environment

- `APP_ENV` — Fixed deployment environment (`dev` or `prod`); forwarded to the browser as `NEXT_PUBLIC_APP_ENV`.
- `NEXT_PUBLIC_OPERATIONS_OPERATOR_DIDS` — Public comma-delimited operator DID allowlist; `OPERATIONS_OPERATOR_DIDS` is also accepted at build time and forwarded for parity with the Operations service.
- `NEXT_PUBLIC_OPERATIONS_GATEWAY_ORIGIN` — The single Gateway origin for this deployment.
- `NEXT_PUBLIC_OPERATIONS_OAUTH_CLIENT_ID` — Hosted client metadata URL when it differs from the same-origin default.
- `NEXT_PUBLIC_OPERATIONS_DEMO_MODE` — Explicit demo mode; never enable in a deployed operator console.

Deploy Development and Production as separate Vercel projects rooted at `apps/operations`. Configure a fixed `APP_ENV`, its corresponding Gateway origin, and the operator DID allowlist per project; the console does not switch environments at runtime. Keep the Production project bound to `https://operations.thesocialwire.app` so it matches `public/operations-client-metadata.json`.

Backfill creation is dry-run-first. Every operator mutation requires an audit note, and Production additionally requires the exact `PRODUCTION` confirmation. Authorization is enforced by the operations service DID allowlist.
