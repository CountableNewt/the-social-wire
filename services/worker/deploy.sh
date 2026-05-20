#!/usr/bin/env bash
# Deploy this Fly app from services/worker (monorepo root = Docker build context).
#
# Usage: bash deploy.sh dev|main
set -euo pipefail

SERVICE_DIR="$(cd "$(dirname "$0")" && pwd)"
BRANCH="${1:?usage: deploy.sh dev|main}"

if [ "$BRANCH" = "main" ]; then
  CONFIG="services/worker/fly.prod.toml"
  APP="${FLY_WORKER_APP_PROD:-the-social-wire-prod-worker}"
else
  CONFIG="services/worker/fly.toml"
  APP="${FLY_WORKER_APP_DEV:-the-social-wire-dev-worker}"
fi

cd "$SERVICE_DIR"
flyctl deploy ../.. --config "$CONFIG" --app "$APP" --remote-only
