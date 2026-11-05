# Shared Swift Packages

The active Gateway, App View, Ingress Controller, Projection Pool, and Coordinator
runtimes build with Swift 6.4.2. Web and iOS retain their existing client stacks.
Shared backend packages remain separate, history-preserving repositories:

| Package | Repository |
| --- | --- |
| GatewayCore | [gateway-core](https://github.com/Stygian-Tech/gateway-core) |
| ThinAppViewCore | [thin-app-view-core](https://github.com/Stygian-Tech/thin-app-view-core) |
| OperationsCore | [operations-core](https://github.com/Stygian-Tech/operations-core) |
| SocialWireRedis | [social-wire-redis](https://github.com/Stygian-Tech/social-wire-redis) |
| WireCore | [wire-core](https://github.com/Stygian-Tech/wire-core) |
| FinanceCore | [finance-core](https://github.com/Stygian-Tech/finance-core) |
| SportsCore | [sports-core](https://github.com/Stygian-Tech/sports-core) |

`ReadStateCore` remains tracked here because the Apple app imports it. The
standalone ThinAppViewCore repository includes a documented snapshot of that
dependency and exports its `ReadStateCore` product for the server dependency graph,
preserving independent builds without moving the active Apple package. Each extracted repository records the source revision and
digests in `SOURCE_PROVENANCE.json`, preserves the license and package history,
and provides its own Swift CI. Database and OAuth test fixtures are bundled
where needed; four fixture-path changes are documented in provenance.

## Runtime Dependencies

Each active service owns a `Package.swift` and committed `Package.resolved`.
Docker builds and CI use `--force-resolved-versions`. Runtime pins are independent
from the historical Swift/Go parity oracle below; changing one does not silently
rewrite the other. Local SwiftPM mirrors are development-only and excluded from
Git and images. Publish package commits before publishing service pins so a clean
checkout can resolve every dependency.

The native ingress package also builds `schema-ready`, used by all four active
images to validate canonical database migration receipts before starting. The
dedicated Database Migrator retains schema ownership. Operations and dormant
corpus-edge services are outside this migration and keep their existing sources.
The retired standalone Tap, Charybdis, and Wire workers remain retired.

Track this migration and its acceptance checks in [GitHub #782](https://github.com/Stygian-Tech/the-social-wire/issues/782).

## Historical Contract Checks

Existing Swift/Go ranking and edition parity, historical source specifications,
and retained Swift contract suites still run. Bootstrap their immutable
reference checkouts before running those checks:

```sh
python3 scripts/go/check-out-swift-contracts.py
python3 scripts/go/package-inventory.py --check
```

`packages/go/migration/extracted-swift-packages.json` pins complete Git revisions.
The checkouts occupy ignored `packages/swift/<Package>` directories so existing
oracle paths stay stable. The bootstrap verifies origin, revision, and clean
status and refuses to overwrite local work. To update a pin, review the new
package revision, preserve any existing checkout changes, replace that generated
checkout deliberately, run bootstrap, and regenerate the source inventory.

The original Go Jetstream golden fixture remains under
`services/jetstream-ingest/internal/ingest/testdata`; native ingress protocol
fixtures and provenance live under `services/jetstream-ingest/Tests`.

Extracting source packages does not reduce deployed Go runtime memory. Memory
work is tracked separately in TSW-154; extraction is TSW-153.
