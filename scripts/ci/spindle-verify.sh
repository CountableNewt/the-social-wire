#!/usr/bin/env bash
set -euo pipefail
# Verification never passes deployment credentials into Docker or test processes.
unset ATCR_DEVICE_SECRET RAILWAY_TOKEN RAILWAY_API_TOKEN
exec node scripts/ci/spindle.mjs verify "${1:-all}"
