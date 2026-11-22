#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repository_root"

swift test --package-path apps/apple/Packages/ReadStateCore

# Select an installed iPhone runtime instead of pinning a device model that can
# disappear when the hosted macOS runner image changes.
simulator_id="$(xcrun simctl list devices available --json | python3 -c '
import json, re, sys
devices = json.load(sys.stdin)["devices"]
for runtime in sorted(devices, key=lambda value: tuple(map(int, re.findall(r"\d+", value))), reverse=True):
    if ".iOS-" not in runtime:
        continue
    for device in devices[runtime]:
        if device.get("isAvailable") and device["name"].startswith("iPhone"):
            print(device["udid"])
            sys.exit(0)
raise SystemExit("No available iPhone simulator")
')"

xcodebuild test \
  -project 'apps/apple/The Social Wire.xcodeproj' \
  -scheme SocialWire \
  -destination "platform=iOS Simulator,id=$simulator_id" \
  -destination-timeout 120 \
  -derivedDataPath "${RUNNER_TEMP:-/tmp}/social-wire-apple-derived" \
  -resultBundlePath "${RUNNER_TEMP:-/tmp}/social-wire-apple-tests.xcresult" \
  -only-testing:SocialWireTests \
  CODE_SIGNING_ALLOWED=NO
