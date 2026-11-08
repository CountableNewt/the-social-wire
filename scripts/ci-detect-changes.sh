#!/usr/bin/env bash
# Detect changed path filters for CI (replaces dorny/paths-filter; no marketplace download).
set -euo pipefail

BASE=""
HEAD="${GITHUB_SHA:?GITHUB_SHA is required}"
MATCH_ALL=0

case "${GITHUB_EVENT_NAME:-}" in
  pull_request)
    BASE="${GITHUB_EVENT_PULL_REQUEST_BASE_SHA:-}"
    if [ -z "$BASE" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
      BASE="$(git merge-base "$HEAD" "origin/${GITHUB_BASE_REF}")"
    fi
    if [ -z "$BASE" ]; then
      echo "Unable to determine pull request base SHA." >&2
      exit 1
    fi
    ;;
  push)
    BASE="${GITHUB_EVENT_BEFORE:-}"
    if [ -z "$BASE" ] || [ "$BASE" = "0000000000000000000000000000000000000000" ]; then
      MATCH_ALL=1
    fi
    ;;
  *)
    MATCH_ALL=1
    ;;
esac

# A workflow or detector change can affect any path decision. Exercise the whole
# matrix so the aggregate gate cannot go green with an untested CI change.
if [ "$MATCH_ALL" = "0" ] && ! git diff --quiet "$BASE" "$HEAD" -- \
  '.github/workflows/ci.yml' \
  '.github/workflows/validated-merge.yml' \
  'scripts/ci-detect-changes.sh' \
  'scripts/ci-prepare-postgres.sh' \
  '.github/actions/prepare-postgres'; then
  MATCH_ALL=1
fi

to_pathspec() {
  local spec="$1"
  if [[ "$spec" == *"*"* ]]; then
    printf ':(glob)%s' "$spec"
  else
    printf '%s' "$spec"
  fi
}

filter_changed() {
  local name="$1"
  shift
  local out="${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"

  if [ "$MATCH_ALL" = "1" ]; then
    echo "${name}=true" >> "$out"
    return
  fi

  local spec pathspec
  for spec in "$@"; do
    pathspec="$(to_pathspec "$spec")"
    if ! git diff --quiet "$BASE" "$HEAD" -- "$pathspec"; then
      echo "${name}=true" >> "$out"
      return
    fi
  done

  echo "${name}=false" >> "$out"
}

filter_changed web \
  'apps/web/**' \
  'packages/vendor/**' \
  'packages/record-keys/**' \
  'packages/read-state/**' \
  'package.json' \
  'bun.lock' \
  'turbo.json' \
  'scripts/check-bun-coverage-inventory.ts' \
  'railway/web.json' \
  '.github/workflows/ci.yml'

filter_changed operations_web \
  'apps/operations/**' \
  'docs/runbooks/operations/**' \
  'package.json' \
  'bun.lock' \
  'turbo.json' \
  'scripts/check-bun-coverage-inventory.ts' \
  'railway/operations-web.json' \
  '.github/workflows/ci.yml'

filter_changed apple \
  'apps/apple/**' \
  'packages/swift/ReadStateCore/**' \
  '.github/workflows/ci.yml'

# Retained Swift package contracts remain independent from retired service runtimes.

filter_changed redis \
  'scripts/ci/check-out-swift-contracts.py' \
  'packages/swift/extracted-swift-packages.json' \
  'packages/swift/SocialWireRedis/**' \
  '.github/workflows/ci.yml'

filter_changed shared_swift \
  'scripts/ci/check-out-swift-contracts.py' \
  'packages/swift/extracted-swift-packages.json' \
  'database/migrations/**' \
  'packages/swift/GatewayCore/**' \
  'packages/swift/OperationsCore/**' \
  'packages/swift/ThinAppViewCore/**' \
  'packages/swift/ReadStateCore/**' \
  'packages/swift/SocialWireRedis/**' \
  'packages/swift/WireCore/**' \
  'packages/swift/FinanceCore/**' \
  'packages/swift/SportsCore/**' \
  '.github/workflows/ci.yml'

filter_changed swift_backend \
  'tools/schema-ready/**' \
  'services/gateway/Dockerfile' \
  'services/gateway/Package.swift' \
  'services/gateway/Package.resolved' \
  'services/gateway/Sources/**' \
  'services/gateway/Tests/**' \
  'services/appview/Package.swift' \
  'services/appview/Dockerfile' \
  'services/appview/Package.resolved' \
  'services/appview/Sources/**' \
  'services/appview/Tests/**' \
  'services/indexing-worker/Package.swift' \
  'services/indexing-worker/Dockerfile' \
  'services/indexing-worker/Package.resolved' \
  'services/indexing-worker/Sources/**' \
  'services/indexing-worker/Tests/**' \
  'database/migrations/**' \
  'railway/gateway.json' \
  'railway/appview.json' \
  '.railway/railway.ts' \
  '.github/workflows/ci.yml'

filter_changed database_migrator \
  'database/migrations/**' \
  'scripts/apply-database-migrations.sh' \
  'scripts/verify-postgres-restore.sh' \
  'scripts/verify-jetstream-v2-drain-indexes.sql' \
  'scripts/verify-wire-inbox-claim-index.sql' \
  'scripts/operations/wire-inbox-readiness.sql' \
  'scripts/verify-wire-corpus-serving.sql' \
  'packages/spec/__tests__/wire-corpus-serving-verifier-postgres.test.ts' \
  'packages/spec/__tests__/wire-inbox-readiness-postgres.test.ts' \
  'services/database-migrator/**' \
  'services/postgres/**' \
  'railway/postgres.json' \
  'railway/database-migrator.json' \
  '.github/workflows/ci.yml'

filter_changed lexicons \
  'packages/lexicons/**' \
  'packages/spec/endpoint-manifest.json' \
  'package.json' \
  'bun.lock' \
  '.github/workflows/ci.yml'

filter_changed spec \
  'scripts/ci/check-out-swift-contracts.py' \
  'packages/swift/extracted-swift-packages.json' \
  'scripts/ci/**' \
  'services/*/Dockerfile' \
  'packages/read-state/**' \
  'packages/spec/**' \
  'scripts/benchmarks/**' \
  'scripts/capture-postgres-cost.sql' \
  'scripts/verify-postgres-restore.sh' \
  '.railway/**' \
  'docs/runbooks/operations/jetstream-v2-durable-replay.md' \
  'packages/lexicons/**' \
  'apps/web/**' \
  'apps/apple/**' \
  'packages/swift/ReadStateCore/**' \
  'railway/**' \
  '**/migrations/**' \
  'services/**/bruno/**' \
  'services/gateway/Sources/Gateway/**' \
  'services/appview/Sources/AppView/**' \
  'packages/swift/GatewayCore/**' \
  'packages/swift/ThinAppViewCore/**' \
  'packages/swift/ReadStateCore/**' \
  'packages/swift/OperationsCore/**' \
  'packages/swift/WireCore/**' \
  'packages/swift/FinanceCore/**' \
  'packages/swift/SportsCore/**' \
  'scripts/apply-database-migrations.sh' \
  'scripts/verify-jetstream-v2-drain-indexes.sql' \
  'scripts/operations/**' \
  'package.json' \
  'bun.lock' \
  '.github/workflows/ci.yml'

filter_changed docs \
  'docs/wiki/**' \
  'scripts/check-wiki-links.sh' \
  '.github/workflows/ci.yml'

filter_changed benchmark_tools \
  'scripts/benchmarks/**' \
  '.github/workflows/ci.yml'

filter_changed podcast_worker \
  'services/podcast-worker/**' \
  'tools/schema-ready/**' \
  'database/migrations/**' \
  '.railway/**' \
  'package.json' \
  'bun.lock'

