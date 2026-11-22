# Testing

The current test surfaces are Web, Go AppView, lexicons, API/client contracts and native Apple. Tests live in the owning package.

| Surface | Command |
|---------|---------|
| Web | `cd apps/web && bun run test:coverage` |
| Go | `cd services/appview && go test ./... && go vet ./...` |
| Lexicons | `cd packages/lexicons && bun test` |
| API contracts | `cd packages/spec && bun test` |
| Native shared package | `cd apps/apple/Packages/ReadStateCore && swift test` |
| Apple app | Xcode SocialWire scheme, simulator tests |

CI verifies packaged Web and Go images and path-filtered native changes. The aggregate required check retains Development-before-Production source parity. Browser acceptance uses Chrome, with signed-in hosted validation performed before promotion.

See [test plans](https://github.com/CountableNewt/the-social-wire/blob/main/docs/test-plans/README.md). Retired Swift server and Postgres migration suites are removed rather than used as current verification.
