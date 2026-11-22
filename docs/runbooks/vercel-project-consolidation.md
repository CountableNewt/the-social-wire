# One-project Vercel cutover (#992)

Source configuration targets the retained `the-social-wire-web` project at the repository root, with Next.js Web and six Go services shipping together. This document is an operator procedure, not evidence that the hosted cutover has happened. Record deployment IDs, resource identities, acceptance results and rollback receipts in #992. Existing hosted projects remain authoritative until acceptance.

## Preview policy

Only `main` and `dev` automatically deploy. Generic branches stay disabled by user decision until isolated resources are provisioned. A manual isolated preview needs two new disposable Turso databases, a private Blob store, independent encryption/job/attestation secrets and preview-specific OAuth/scheduling origins. It must receive no Production or shared Development credentials. Initialize these disposable stores explicitly outside the build; leave `DEPLOY_SCHEMA_INITIALIZATION=false`. Never pull the project's unscoped Preview variables into this deployment. Verify the complete resolved environment before building. Preview deployment protection must permit OAuth metadata discovery without exposing private data.

No generic preview declares schedules. The final root config declares a quarter-hour dispatcher only for `APP_ENV=prod`, Production target, `main`, and an hourly dispatcher only for `APP_ENV=dev`, Preview target, `dev`. Both serving and build behavior retain the existing readiness and initialization guards.

## Prepare the retained project

1. Inventory both projects' settings, domain/branch assignments, current deployment IDs, schedules and environment variable names/scopes. Keep a private recovery copy of values; never print secrets or place them in Git. Preserve Web Analytics and Production Speed Insights on the retained Web project.
2. Reconnect the existing environment-specific Turso and private Blob resources to the retained project. Copy backend variables into matching Production and branch-scoped `dev` Preview targets without changing endpoints, object store identities, auth tokens, `SOCIAL_DRAFT_STORAGE_KEY`, `SCHEDULING_STORAGE_KEY`, attestation secrets or analytics HMAC key. Preserve Web server-only L@tr configuration. Resolve name collisions explicitly; do not replace either environment with the other.
3. Configure project Root Directory as repository root and clear project-level framework/build/install/output overrides. Root `vercel.ts` owns service configuration. Web installs the frozen root workspace; Go builds still run from `services/appview`. No store initialization is needed for this topology-only move.
4. Browser API traffic uses its current deployment origin. Remove obsolete hosted `NEXT_PUBLIC_SOCIALWIRE_API_URL` overrides. Keep canonical API OAuth/native metadata and scheduling origins unchanged. Set explicit frontend CORS origins for each environment. Keep native client IDs and redirect schemes unchanged.
5. Keep Git deployment restricted while preparing the cutover. A manual CLI archive must contain the complete workspace, root lockfile and local vendor packages. Render `APP_ENV=… VERCEL_ENV=… VERCEL_GIT_COMMIT_REF=… bun scripts/render-vercel-config.ts` into that private archive's sole root `vercel.json`, omitting its `vercel.ts`. Run the CLI from that archive linked to the verified retained project. Never deploy the six-service backend directory alone.

## Validate an isolated deployment

Use Chrome. Confirm frontend pages and Next-owned `/api/*` handlers reach Web, backend namespaces reach their existing Go owners, and API-host metadata reaches reader. Web `/oauth-client-metadata.json` must retain a same-origin `/callback`; API/native metadata must retain existing stable client identities. Unknown API-host paths must return the backend response, not frontend HTML.

Inspect Chrome Network and service logs to prove every gateway request stays in the same deployment. Sign in and verify bootstrap NDJSON arrives progressively, read-state updates, drafts, publishing, encrypted attachment chunk upload/download, scheduling OAuth, analytics and integrations. Confirm first-write nonce challenge retries succeed. Confirm forged forwarded/original URL headers cannot satisfy DPoP for a different host, path or method. Verify native API routes and client metadata, CORS preflight, TLS and callback redirects. Check the root Analytics mount and Production Speed Insights assignment.

## Development, then Production

1. Disable the old AppView project's Development dispatcher and drain outstanding invocations before enabling the retained project's Development dispatcher. Durable leases are additional protection, not proof that duplicate ownership is safe.
2. Deploy the reviewed source to `dev` with Development-only credentials. Reassign `api.testing.thesocialwire.app` to the retained project's `dev` deployment; retain `testing.thesocialwire.app`. Validate the full acceptance above and record unchanged database/object identities plus exactly one hourly dispatcher. Exercise rollback to the prior accepted deployment and restore the tested candidate.
3. Only after Development acceptance and required patch-parity CI, repeat for `main` Production: stop/drain old Production maintenance, stage the Production deployment, reassign `api.thesocialwire.app`, and preserve `thesocialwire.app` plus `www.thesocialwire.app`. Verify TLS/aliases and the single quarter-hour dispatcher. Do not change plan or billing settings.
4. Retire the old AppView project only after full acceptance and recovery checks. Preserve all Turso databases, Blob stores, encryption keys and recovery artifacts. Project deletion is a separate destructive operator action.

## Rollback

Keep both prior project deployment IDs and domain/branch assignments. Stop replacement maintenance and drain requests before restoring old ownership. If the old project's root is unchanged, redeploying current source there is unsupported after consolidation; use the retained previous deployment. Restore API domain assignments and the single old dispatcher only after the replacement is idle. For the retained project, restore its previous root/build settings if returning to its prior Web-only deployment. Because data and keys remain unchanged, rollback requires no data copy; inspect any schema/behavior compatibility changes independently. Verify reader, encrypted attachments, scheduling and domain/OAuth identities again.
