# Deployment and Environments

Development tracks `dev`; Production tracks `main`. Required CI enforces Development-first patch parity. Root Vercel Services configuration targets the retained Web project with Next.js Web and six Go AppView services shipping and rolling back together. Issue #992 tracks hosted consolidation acceptance; source changes alone do not reassign domains or retire projects.

| Environment | Web | Go AppView |
|-------------|-----|------------|
| Development | `testing.thesocialwire.app` | `api.testing.thesocialwire.app` |
| Production | `thesocialwire.app`, `www.thesocialwire.app` | `api.thesocialwire.app` |

Use root `vercel.ts` and `scripts/vercel-config.ts`, with repository-root project settings. Generic branches remain disabled until isolated Turso databases, private Blob storage and credentials are provisioned. Browser API calls stay on their current deployment's origin. Native API identities remain on the existing API domains.

Each release environment retains its existing two US Turso databases, private Blob store and encryption keys. No database copy, object copy or plan change is required for consolidation. Preview builds never initialize shared stores. Disable and drain the old AppView dispatcher before enabling the retained project's replacement; verify exactly one dispatcher per environment.

Preserve Web Analytics and Production Speed Insights on the retained project. Keep prior deployments, domain assignments, keys and resource identities for rollback. Retire the old project only after Development then Production acceptance and recovery checks; never delete live data or recovery artifacts as part of project cleanup.

See the [consolidation runbook](https://github.com/CountableNewt/the-social-wire/blob/main/docs/runbooks/vercel-project-consolidation.md) and [runtime guide](https://github.com/CountableNewt/the-social-wire/blob/main/docs/architecture/standalone-reader.md).
