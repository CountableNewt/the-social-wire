# OpenAPI spec

OpenAPI compatibility contract and transport manifest for the standalone Go backend in `services/appview`. Lexicon-defined service XRPC schemas live under `packages/lexicons/app/thesocialwire/`.

[`openapi.yaml`](./openapi.yaml) uses OpenAPI 3.1. [`endpoint-manifest.json`](./endpoint-manifest.json) classifies every documented operation and Next.js BFF surface. `xrpc-migration` entries name the exact Lexicon NSID; operational, metadata, streaming, media, ATProto-repository, foreign-service, and vendor-owned HTTP endpoints remain explicitly classified outside XRPC.

## Purpose

Documents the Go edge's health, OAuth metadata, sync cache, publication sidebar, reader bootstrap/feed/read-state, analytics, drafts, scheduling and integrated L@tr/Semble routes. User-authored PDS records remain authoritative; local SQLite holds derived projections and private service state.

Manual requests live under `services/appview/bruno/`, including the former Gateway requests and post-ingestion verification requests. Operations Web and its control-plane API are retired.

## Server URLs

Production uses `api.thesocialwire.app`; Development uses `api.testing.thesocialwire.app`. Local backend development normally uses port 8081.

## Drift check

```bash
cd packages/spec
bun test
```

Checks documented methods and paths against Go registrations in both directions, including literal registrations, constant prefixes, string loops and the reader compatibility/XRPC route table. Unknown registration expressions fail closed. The endpoint manifest must classify every OpenAPI operation exactly once.

CI runs these contracts as part of Web verification. Native consumers remain covered by the Apple job.

## Related

- [AppView service](../../services/appview/)
