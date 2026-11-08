# Tangled delivery

The canonical target is `https://tangled.org/samclemente.me/the-social-wire`.
Select a Spindle with the documented `microvm` engine, a NixOS image, Docker,
and a workflow budget sufficient for a clean Swift build and Web tests (at
least 60 minutes). The operator's default five-minute timeout is insufficient.
Do not remove the GitHub required gate until hosted Spindle runs are accepted.
Native iOS builds and CI/CD are handled separately by explicit user decision;
these workflows cover only Web, the Swift backend and its media processor.

`verify.yml` checks feature pushes and pull requests with pinned Docker build
stages. `release.yml` verifies again before publishing on `dev` or `main`; it
never relies on another workflow finishing first. Both use the same
`scripts/ci/spindle-verify.sh` / `spindle.mjs` entrypoints that can be run locally.

Configure these repository settings on the selected Spindle:

| Name | Value |
| --- | --- |
| `ATCR_NAMESPACE` | ATProto handle owning the images |
| `ATCR_DEVICE_USERNAME` | Handle used for ATCR Docker login |
| `ATCR_DEVICE_SECRET` | Dedicated device secret created at `https://atcr.io/settings/devices` |
| `RAILWAY_PROJECT_ID` | Existing Social Wire project UUID |
| `RAILWAY_DEVELOPMENT_ENVIRONMENT_ID` | Development environment UUID |
| `RAILWAY_DEVELOPMENT_WEB_SERVICE_ID` | Development Web UUID |
| `RAILWAY_DEVELOPMENT_APPVIEW_SERVICE_ID` | Development consolidated backend UUID |
| `RAILWAY_PRODUCTION_ENVIRONMENT_ID` | Production environment UUID |
| `RAILWAY_PRODUCTION_WEB_SERVICE_ID` | Production Web UUID |
| `RAILWAY_PRODUCTION_APPVIEW_SERVICE_ID` | Production consolidated backend UUID |
| `RAILWAY_API_TOKEN` | Workspace-scoped API token able to inspect both environments and update the two services; use the bearer-token header |

Railway project tokens are limited to one environment. `RAILWAY_TOKEN` is supported
for Development-only deployments, but cannot satisfy Production's cross-environment
acceptance check. Configure exactly one token type; a workspace API token supports
the full pipeline without an account-wide token.

Optional `SPINDLE_WEB_BUILD_ARGS` is a JSON object containing extra public
`NEXT_PUBLIC_*` strings supported by the Web Dockerfile. Branch selection fixes
the environment, site, API URL and thin-AppView flag; extras cannot override
them. Never put a secret in a public build argument.

Spindle currently injects repository secrets into every trusted workflow.
Verification clears deployment credentials before invoking tests/containers;
release credentials are used only by the branch release entrypoint. Untrusted
fork/patch runs receive no secrets from Spindle. Push access therefore remains
credential access; Spindle does not provide a per-workflow secret allowlist.

The pipeline builds `linux/amd64` images in ATCR repositories
`the-social-wire-web` and `the-social-wire-appview`. Railway receives references
containing both a full commit tag and digest, for example
`atcr.io/<handle>/the-social-wire-appview:dev-<40-character-sha>@sha256:<digest>`.
Use that format for the initial operator-managed source cutover too. Production
release checks require successful Development deployments of both services at
current `dev`, and apply the existing equivalent-patch guard from each deployed
Production baseline. An incomplete deployment blocks another release until it
is resolved; a superseded branch build cannot update Railway.

ATCR is not covered by Railway's registry auto-update feature. Each release
explicitly updates only the two service image sources, starts deployments, and
waits for the exact returned deployment IDs to reach `SUCCESS`. It then checks
the source digest and deployment ID again. Pending approval, sleeping, skipped,
failed, or timed-out runs are not reported as successful. Private image pulls
also require ATCR registry credentials configured separately on Railway; never
put the publishing credential into a Dockerfile.

References: [Spindle workflows](https://tangled.org/docs/spindles/workflows/),
[secret behavior](https://tangled.org/docs/spindles/secrets/),
[Railway image sources](https://docs.railway.com/builds/private-registries),
[Railway auto-update coverage](https://docs.railway.com/deployments/image-auto-updates).
