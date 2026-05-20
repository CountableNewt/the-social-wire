#!/usr/bin/env bash
# Deploy the Thin AppView worker to Fly.io for dev or main.
#
# Requires: FLY_API_TOKEN, FLY_WORKER_APP_DEV / FLY_WORKER_APP_PROD.
# Usage: bash scripts/fly-deploy-worker.sh dev|main
set -euo pipefail

BRANCH="${1:?usage: fly-deploy-worker.sh dev|main}"
CONFIG="${FLY_WORKER_CONFIG:-}"
if [ -z "$CONFIG" ]; then
  if [ "$BRANCH" = "main" ]; then
    CONFIG="services/worker/fly.prod.toml"
  else
    CONFIG="services/worker/fly.toml"
  fi
fi

if [ -z "${FLY_API_TOKEN:-}" ]; then
  echo '::error::Missing FLY_API_TOKEN.'
  exit 1
fi

if [ ! -f "$CONFIG" ]; then
  echo "::error::Missing worker config: ${CONFIG}"
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

echo "::notice::Fly worker deploy → ${APP} (${BRANCH})"
flyctl deploy . --config "$CONFIG" --app "$APP" --remote-only
