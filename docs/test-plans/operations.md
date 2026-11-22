> Historical reference: this document describes infrastructure retired from source in #963. Do not apply its deployment commands. Current runtime and validation guidance is in `docs/architecture/standalone-reader.md` and `docs/test-plans/README.md`.

# Operations and ingestion-recovery test plan

The operator surfaces span the shared Swift control-plane package, the Operations service, and Jetstream V2 ingestion/recovery evidence. Tap-specific cases are retained as historical compatibility coverage only.

## Local commands

Run from the monorepo root:

```bash
bun install
(cd packages/swift/OperationsCore && swift test)
(cd services/operations && swift test)
(cd services/jetstream-ingest && go test ./... && go vet ./...)
```

## CI coverage

| Job | Coverage |
|-----|----------|
| `operations` | `OperationsCore` and `services/operations` Swift Testing suites plus the production image build |
| `jetstream-ingest` | Race-enabled Go unit and live-Postgres integration tests, statement coverage gate, vet, and the Jetstream V2 image build |

## Manual checks

- [ ] Production mutations require the exact `PRODUCTION` confirmation and preserve audit evidence
- [ ] Jetstream V2 Ingest holds the environment-scoped fenced lease and Charybdis reports the intended V1/V2 authority mode
- [ ] A mode change produces a non-skipped Charybdis deployment whose startup log reports the intended `jetstream_mode`
- [ ] V2 authority evidence resolves from the durable checkpoint/inbox without requiring a legacy `jetstream` stream row
- [ ] V2 transport freshness comes from the active fenced ingester lease, not projection-owned checkpoint updates
- [ ] V2 backlog alerts use only the advertised source generation while aggregate retained-generation evidence remains visible
- [ ] Charybdis `/readyz` and aggregate Gateway `/readyz` are evaluated independently so an App View or Gateway database failure is not attributed to ingestion
- [ ] Charybdis Railway deployment reaches `SUCCESS` through database-only `/startupz`, then Charybdis `/readyz` independently proves fresh authoritative transport and projection evidence

## Related

- [Operations runbooks](../runbooks/operations/README.md)
- [Retired Tap cutover history](../runbooks/operations/tap-shadow-and-cutover.md)
