#!/usr/bin/env bash
set -euo pipefail
# Verification never passes deployment credentials into Docker or test processes.
unset RAILWAY_TOKEN RAILWAY_API_TOKEN GITHUB_TOKEN GH_TOKEN
exec node scripts/ci/verify-runtime-images.mjs "${1:-all}"
