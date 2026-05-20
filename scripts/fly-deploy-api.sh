#!/usr/bin/env bash
# Deploy services/api to Fly.io for dev or main.
#
# Requires: FLY_API_TOKEN, FLY_APP_DEV / FLY_APP_PROD (or pass branch).
# Usage: bash scripts/fly-deploy-api.sh dev|main
set -euo pipefail

BRANCH="${1:?usage: fly-deploy-api.sh dev|main}"
CONFIG="${FLY_API_CONFIG:-}"

if [ -z "$CONFIG" ]; then
  if [ "$BRANCH" = "main" ]; then
    CONFIG="services/api/fly.prod.toml"
  else
    CONFIG="services/api/fly.toml"
  fi
fi

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

echo "::notice::Fly deploy → ${APP} (${BRANCH})"
flyctl deploy . --config "$CONFIG" --app "$APP" --remote-only
