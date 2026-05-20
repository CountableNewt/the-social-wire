#!/usr/bin/env bash
# Deploy this Fly app from services/api (monorepo root = Docker build context).
#
# Usage: bash deploy.sh dev|main
set -euo pipefail

SERVICE_DIR="$(cd "$(dirname "$0")" && pwd)"
BRANCH="${1:?usage: deploy.sh dev|main}"

if [ "$BRANCH" = "main" ]; then
  CONFIG="services/api/fly.prod.toml"
  APP="${FLY_APP_PROD:-the-social-wire-prod}"
else
  CONFIG="services/api/fly.toml"
  APP="${FLY_APP_DEV:-the-social-wire-dev}"
fi

cd "$SERVICE_DIR"
flyctl deploy ../.. --config "$CONFIG" --app "$APP" --remote-only
