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
  'scripts/ci-detect-changes.sh'; then
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

# Web verification includes source-reading API/schema contracts and shared packages.
filter_changed web \
  'apps/web/**' \
  'apps/apple/**' \
  'packages/**' \
  'services/**' \
  'scripts/**' \
  'tools/**' \
  'docs/runbooks/**' \
  'vercel.ts' \
  'scripts/vercel-config.ts' \
  'package.json' \
  'bun.lock' \
  'turbo.json' \
  '.dockerignore'

# Only the consolidated backend is a runtime target.
filter_changed appview \
  'services/appview/**' \
  'apps/web/package.json' \
  'packages/read-state/package.json' \
  'packages/lexicons/package.json' \
  'packages/spec/package.json' \
  'packages/spec/openapi.yaml' \
  'packages/vendor/**' \
  'scripts/ci/**' \
  'vercel.ts' \
  'scripts/vercel-config.ts' \
  'package.json' \
  'bun.lock' \
  '.dockerignore'

# Native client and its local ReadStateCore package are verified independently.
filter_changed apple \
  'apps/apple/**' \
  'packages/lexicons/**' \
  'packages/spec/openapi.yaml' \
  'scripts/ci/apple-test.sh'
