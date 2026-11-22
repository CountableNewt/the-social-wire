#!/usr/bin/env bash
set -euo pipefail

# The standalone Go builder supplies this destination for its IPC server wrapper.
: "${VERCEL_OUTPUT_FILE:?Vercel must provide the Go server output path}"

# Accept only the fixed deployment surfaces, never a caller-supplied Go path.
surface="${1:-}"
case "$surface" in
  reader|publishing|scheduling|analytics|integrations|maintenance) ;;
  *) echo "A supported AppView surface is required." >&2; exit 1 ;;
esac
go build -trimpath -ldflags='-s -w' -o "$VERCEL_OUTPUT_FILE" "./cmd/$surface"

# Services has no preDeployCommand. Explicit release initialization runs after
# compilation and before activation, never on a Preview build. Preview stores
# need their own explicit external initialization; serving only checks readiness.
if [[ "$surface" != "maintenance" || "${DEPLOY_SCHEMA_INITIALIZATION:-false}" != "true" ]]; then
  exit 0
fi
if [[ "${VERCEL_ENV:-}" != "production" ]]; then
  echo "Schema initialization requires a production-target release build." >&2
  exit 1
fi
case "${APP_ENV:-}:${VERCEL_GIT_COMMIT_REF:-}" in
  dev:dev|prod:main) ;;
  *)
    echo "Schema initialization requires the matching Development or Production source branch." >&2
    exit 1
    ;;
esac
"$VERCEL_OUTPUT_FILE" --initialize-only
