#!/usr/bin/env bash
# Verify Supabase CI database credentials without applying migrations.
#
# Load from act secrets or export manually:
#   SUPABASE_ACCESS_TOKEN
#   SUPABASE_DEV_PROJECT_REF / SUPABASE_PROD_PROJECT_REF
#   SUPABASE_*_DATABASE_URL  (session pooler URI preferred)
#   SUPABASE_*_DB_PASSWORD   (plain password; same as in URI after rotation)
#
# Usage:
#   bash scripts/supabase-verify-connection.sh dev
set -euo pipefail

ENV_LABEL="${1:?usage: supabase-verify-connection.sh dev|prod}"
env_upper="$(printf '%s' "$ENV_LABEL" | tr '[:lower:]' '[:upper:]')"

trim_secret() {
  local v="$1"
  v="$(printf '%s' "$v" | tr -d '\r\n')"
  printf '%s' "$v"
}

REF="$(trim_secret "${REF:-}")"
if [ -z "$REF" ]; then
  ref_var="SUPABASE_${env_upper}_PROJECT_REF"
  REF="$(trim_secret "${!ref_var:-}")"
fi

DATABASE_URL="$(trim_secret "${DATABASE_URL:-}")"
if [ -z "$DATABASE_URL" ]; then
  url_var="SUPABASE_${env_upper}_DATABASE_URL"
  DATABASE_URL="$(trim_secret "${!url_var:-}")"
fi

DB_PASSWORD="$(trim_secret "${DB_PASSWORD:-}")"
if [ -z "$DB_PASSWORD" ]; then
  pass_var="SUPABASE_${env_upper}_DB_PASSWORD"
  DB_PASSWORD="$(trim_secret "${!pass_var:-}")"
fi

is_direct() {
  [[ "$1" =~ @db\.[^/@]+\.supabase\.co ]]
}

echo "==> Supabase **${ENV_LABEL}** connection check (dry-run only)"

if [ -n "$DATABASE_URL" ] && ! is_direct "$DATABASE_URL"; then
  echo "→ Using session pooler DATABASE_URL"
  supabase db push --db-url "$DATABASE_URL" --dry-run --yes
  echo "OK: pooler DATABASE_URL works."
  exit 0
fi

if [ -n "$DATABASE_URL" ] && is_direct "$DATABASE_URL"; then
  echo "WARN: SUPABASE_${env_upper}_DATABASE_URL is still db.*.supabase.co (CI ignores this)."
fi

if [ -z "$DB_PASSWORD" ]; then
  echo "error: set SUPABASE_${env_upper}_DATABASE_URL to the session pooler URI, or SUPABASE_${env_upper}_DB_PASSWORD." >&2
  exit 1
fi

if [ -z "${SUPABASE_ACCESS_TOKEN:-}" ] || [ -z "$REF" ]; then
  echo "error: need SUPABASE_ACCESS_TOKEN and SUPABASE_${env_upper}_PROJECT_REF for password fallback." >&2
  exit 1
fi

echo "→ Using supabase link + DB_PASSWORD (pooler)"
export SUPABASE_DB_PASSWORD="$DB_PASSWORD"
supabase link --project-ref "$REF" --yes
supabase db push --dry-run --yes
echo "OK: link + password works."
