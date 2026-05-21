#!/usr/bin/env bash
# Create Fly.io apps for Social Wire gateway, appview, and appview-worker (dev + prod).
#
# Prereqs: flyctl installed and logged in (`fly auth login`).
# Optional: FLY_ORG=your-org-name (else uses flyctl default org).
#
# Usage:
#   bash scripts/fly-create-apps.sh
#   FLY_ORG=stygian-tech bash scripts/fly-create-apps.sh
set -euo pipefail

REGION="${FLY_REGION:-ams}"

GATEWAY_DEV="${FLY_GATEWAY_APP_DEV:-the-social-wire-dev-gateway}"
GATEWAY_PROD="${FLY_GATEWAY_APP_PROD:-the-social-wire-prod-gateway}"
APPVIEW_DEV="${FLY_APPVIEW_APP_DEV:-the-social-wire-dev-appview}"
APPVIEW_PROD="${FLY_APPVIEW_APP_PROD:-the-social-wire-prod-appview}"
WORKER_DEV="${FLY_APPVIEW_WORKER_APP_DEV:-the-social-wire-dev-appview-worker}"
WORKER_PROD="${FLY_APPVIEW_WORKER_APP_PROD:-the-social-wire-prod-appview-worker}"

ensure_flyctl() {
  if command -v flyctl >/dev/null 2>&1; then
    return 0
  fi
  if [ -x "${HOME}/.fly/bin/flyctl" ]; then
    export PATH="${HOME}/.fly/bin:${PATH}"
    return 0
  fi
  echo "error: flyctl not found. Install: https://fly.io/docs/flyctl/install/" >&2
  exit 1
}

fly_auth_ok() {
  if flyctl auth whoami >/dev/null 2>&1; then
    flyctl auth whoami
    return 0
  fi
  echo "error: not logged in to Fly. Run: fly auth login" >&2
  exit 1
}

app_exists() {
  local name="$1"
  flyctl apps list --json 2>/dev/null | python3 -c "
import json, sys
name = sys.argv[1]
data = json.load(sys.stdin)
print('yes' if any(a.get('Name') == name or a.get('name') == name for a in data) else 'no')
" "$name"
}

create_app() {
  local name="$1"
  if [ "$(app_exists "$name")" = "yes" ]; then
    echo "→ ${name}: already exists"
    return 0
  fi

  echo "→ ${name}: creating (primary region ${REGION} for first deploy)"
  if [ -n "${FLY_ORG:-}" ]; then
    flyctl apps create "$name" --org "$FLY_ORG"
  else
    flyctl apps create "$name"
  fi
}

ensure_flyctl
fly_auth_ok

echo "==> Creating Fly apps (region ${REGION} for first deploy)"
create_app "$GATEWAY_DEV"
create_app "$GATEWAY_PROD"
create_app "$APPVIEW_DEV"
create_app "$APPVIEW_PROD"
create_app "$WORKER_DEV"
create_app "$WORKER_PROD"

echo ""
echo "==> Done. Set GitHub Actions secrets (optional overrides):"
echo "  FLY_GATEWAY_APP_DEV=${GATEWAY_DEV}"
echo "  FLY_GATEWAY_APP_PROD=${GATEWAY_PROD}"
echo "  FLY_APPVIEW_APP_DEV=${APPVIEW_DEV}"
echo "  FLY_APPVIEW_APP_PROD=${APPVIEW_PROD}"
echo "  FLY_APPVIEW_WORKER_APP_DEV=${WORKER_DEV}"
echo "  FLY_APPVIEW_WORKER_APP_PROD=${WORKER_PROD}"
echo ""
echo "Next: fly secrets set on each app (SUPABASE_DATABASE_URL, APP_ENV, ENABLE_THIN_APPVIEW, APPVIEW_BASE_URL on gateway, …)."
echo "Deploy: CI on push, or bash scripts/fly-deploy-gateway.sh dev && bash scripts/fly-deploy-appview.sh dev && bash scripts/fly-deploy-appview-worker.sh dev"
