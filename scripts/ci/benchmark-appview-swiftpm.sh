#!/usr/bin/env bash
set -euo pipefail

lane=${1:?expected tests or release}
iteration=${2:?expected initial or edit}
platform=linux/amd64
image=appview-benchmark-swift
dockerfile=services/appview/Dockerfile.standalone
scratch=${RUNNER_TEMP:?}/appview-swiftpm
output=${RUNNER_TEMP}/appview-artifacts

case "$lane/$iteration" in
  tests/initial|tests/edit|release/initial|release/edit) ;;
  *) exit 2 ;;
esac

timed() {
  local phase=$1 started status=0
  shift
  started=$(date +%s)
  "$@" || status=$?
  local seconds=$(( $(date +%s) - started ))
  printf 'AppView benchmark: %s %s %s %ss exit=%s\n' "$lane" "$iteration" "$phase" "$seconds" "$status"
  if [[ -n ${GITHUB_STEP_SUMMARY:-} ]]; then
    printf -- '- %s / %s / %s: %s seconds (exit %s)\n' "$lane" "$iteration" "$phase" "$seconds" "$status" >> "$GITHUB_STEP_SUMMARY"
  fi
  return "$status"
}

mkdir -p "$scratch" "$output"

swift_container() {
  docker run --rm --platform "$platform" --user "$(id -u):$(id -g)" \
    --env HOME=/tmp --mount "type=bind,source=$PWD,target=/src" \
    --mount "type=bind,source=$scratch,target=/swiftpm" \
    --mount "type=bind,source=$output,target=/artifacts" \
    "$image" "$@"
}

if [[ "$iteration" == initial ]]; then
  timed toolchain docker buildx build --platform "$platform" --file "$dockerfile" \
    --target swift-toolchain --load --tag "$image" .
  if [[ "$lane" == tests ]]; then
    swift_container env PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts/ci -p test_prepare_swift_tests.py
    python3 scripts/ci/prepare-swift-tests.py .
    timed migrations swift_container env PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts/migrations -p 'test_*.py'
  fi
else
  # Verify the exact revision first, then exercise a Swift source edit against
  # its warm state. Restore the file after the edited build has been measured.
  benchmark_source=services/appview/Sources/AppView/Analytics/PublicationAnalyticsCountReader.swift
  cp -p "$benchmark_source" "$RUNNER_TEMP/appview-benchmark-source"
  trap 'mv "$RUNNER_TEMP/appview-benchmark-source" "$benchmark_source"' EXIT
  printf '\n// SwiftPM incremental benchmark input.\n' >> "$benchmark_source"
fi

swift_options=(--package-path services/appview --scratch-path /swiftpm --build-system native
  --manifest-cache local --force-resolved-versions
  -Xswiftc -enable-incremental-file-hashing -j "$(getconf _NPROCESSORS_ONLN)")
if [[ "$lane" == tests ]]; then
  timed compile swift_container swift build --build-tests "${swift_options[@]}"
  timed tests swift_container swift test --skip-build "${swift_options[@]}"
  if [[ "$iteration" == initial ]]; then
    timed media docker buildx build --platform "$platform" --file "$dockerfile" --target media-tests .
  fi
else
  swift_options+=(-c release -Xswiftc -no-whole-module-optimization -Xswiftc -incremental)
  timed compile swift_container swift build --product AppView "${swift_options[@]}"
  swift_container bash -euc '
    build_dir=$(swift build --show-bin-path "$@")
    mkdir -p /artifacts/out/bin /artifacts/out/lib
    cp "$build_dir/AppView" /artifacts/out/bin/
    cp -a /usr/lib/swift/linux/*.so* /artifacts/out/lib/
    find -L "$build_dir" -maxdepth 1 -type d \( -name "*.resources" -o -name "*.bundle" \) -exec cp -aL {} /artifacts/out/bin/ \;
  ' bash "${swift_options[@]}"
  runtime_image=appview-swiftpm-benchmark:runtime
  timed package docker buildx build --platform "$platform" --file "$dockerfile" --load \
    --tag "$runtime_image" --build-context "benchmark-artifacts=$output" \
    --build-arg APPVIEW_ARTIFACTS=benchmark-artifacts .
  timed smoke node --input-type=module - "$runtime_image" <<'JS'
import { runtimeSmoke, runtimeVolumeGuardSmoke } from './scripts/ci/verify-runtime-images.mjs';
await runtimeVolumeGuardSmoke(process.argv[2]);
await runtimeSmoke(process.argv[2]);
JS
fi
du -sh "$scratch"
