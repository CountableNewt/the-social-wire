#!/usr/bin/env bash
# Deploy the independent operations control plane to Fly.io.
set -euo pipefail

BRANCH="${1:?usage: fly-deploy-operations.sh dev|main}"
if [ -z "${FLY_API_TOKEN:-}" ]; then
  echo '::error::Missing FLY_API_TOKEN.'
  exit 1
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
echo "::notice::Fly operations deploy (${BRANCH})"
exec bash "$ROOT/services/operations/deploy.sh" "$BRANCH"
