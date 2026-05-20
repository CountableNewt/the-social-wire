#!/usr/bin/env bash
# Deploy the Thin AppView worker to Fly.io for dev or main.
#
# Requires: FLY_API_TOKEN, FLY_WORKER_APP_DEV / FLY_WORKER_APP_PROD.
# Usage: bash scripts/fly-deploy-worker.sh dev|main
set -euo pipefail

BRANCH="${1:?usage: fly-deploy-worker.sh dev|main}"

if [ -z "${FLY_API_TOKEN:-}" ]; then
  echo '::error::Missing FLY_API_TOKEN.'
  exit 1
fi

if [ "$BRANCH" = "main" ]; then
  APP="${FLY_WORKER_APP_PROD:-}"
else
  APP="${FLY_WORKER_APP_DEV:-}"
fi

if [ -z "$APP" ]; then
  echo '::error::Missing worker Fly app — set FLY_WORKER_APP_DEV / FLY_WORKER_APP_PROD.'
  exit 1
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
echo "::notice::Fly worker deploy → ${APP} (${BRANCH})"
cd "$ROOT/services/worker"
exec bash deploy.sh "$BRANCH"
