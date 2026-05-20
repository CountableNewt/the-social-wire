#!/usr/bin/env bash
# Deploy services/api to Fly.io for dev or main.
#
# Requires: FLY_API_TOKEN, FLY_APP_DEV / FLY_APP_PROD.
# Usage: bash scripts/fly-deploy-api.sh dev|main
set -euo pipefail

BRANCH="${1:?usage: fly-deploy-api.sh dev|main}"

if [ -z "${FLY_API_TOKEN:-}" ]; then
  echo '::error::Missing FLY_API_TOKEN.'
  exit 1
fi

if [ "$BRANCH" = "main" ]; then
  APP="${FLY_APP_PROD:-}"
else
  APP="${FLY_APP_DEV:-}"
fi

if [ -z "$APP" ]; then
  echo '::error::Missing Fly app name — set FLY_APP_DEV / FLY_APP_PROD.'
  exit 1
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
echo "::notice::Fly deploy → ${APP} (${BRANCH})"
cd "$ROOT/services/api"
exec bash deploy.sh "$BRANCH"
