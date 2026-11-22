# Test Plans

Tests live in the owning package. Run verification before release; #963 source preparation currently defers execution at the user's request.

| Surface | Command | CI |
|---------|---------|----|
| Web | `cd apps/web && bun run test:coverage && bun run typecheck && bun run lint && bun run build` | Web image |
| Go AppView | `cd services/appview && go test ./... && go vet ./...` | AppView image |
| Lexicons | `cd packages/lexicons && bun test` | Web image |
| OpenAPI and client contracts | `cd packages/spec && bun test` | Web image |
| Native ReadStateCore | `cd apps/apple/Packages/ReadStateCore && swift test` | Apple |
| Apple app | Xcode SocialWire scheme, simulator unit tests | Apple |

The retired Swift server, Postgres migration and Operations Web test surfaces are removed. SQLite migration tests live with their Go stores. New API behavior requires Bruno requests in `services/appview/bruno` and Go-backed OpenAPI drift coverage.

The `CI — Required` gate retains Development-before-Production patch parity. Hosted authenticated Web/iOS acceptance and database restore rehearsals are release checks; a successful build is not a substitute. Validate browsers in Chrome.

See [the standalone backend runbook](../architecture/standalone-reader.md), [the native app guide](../../apps/apple/README.md), and [contributing](../../CONTRIBUTING.md).
