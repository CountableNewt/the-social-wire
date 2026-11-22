# Architecture Overview

The Social Wire has thin Next.js and native SwiftUI clients, a Go AppView API, US Turso databases and private encrypted Blob storage. User-owned protocol records remain on each viewer's ATProto PDS.

```text
Web (Vercel) / Apple client
    |
    +---- viewer PDS: portable user records and publishing
    +---- public ATProto: identity, graph and publisher records
    `---- Go AppView (Vercel): OAuth/DPoP and application APIs
              |
              +---- Turso AppView: projections and private application state
              +---- Turso scheduling: encrypted sessions and leased jobs
              +---- private Blob: server-encrypted draft attachment objects
              `---- bounded RSS/discovery and external integrations
```

## Runtime and Data Ownership

The backend project has six Go Functions: reader, publishing, scheduling, analytics, integrations and maintenance. Shared source lives in `services/appview/internal/appviewruntime`. Existing API paths and stable public domains remain unchanged across hosting providers. Bootstrap uses authenticated NDJSON without buffering. Server-side projection and unread logic remain shared by Web and Apple clients.

The viewer's PDS remains authoritative for user-authored protocol records. Publisher PDS records remain authoritative for standard.site content. AppView stores rebuildable content/projection data and private unpublished state. AppView and scheduling use distinct databases and encryption keys remain unchanged during migration. Browser and Apple caches are per-device, rebuildable state.

## Hosting and Releases

Exactly two Vercel projects host Web (`apps/web`) and AppView (`services/appview`). Both use `main` for Production and the `dev` branch Preview for Development. Environment-specific domains, database tokens, private Blob stores and credentials stay isolated. Other preview branches do not inherit Development scheduling or shared database initialization.

Functions run in `sfo1`; database and object placement must remain in the United States. The maintenance Function uses a consolidated authenticated schedule with durable per-action leases and cadence fences. Production runs every 15 minutes and Development hourly; delivery is approximate. Serving never applies remote schema DDL. Initialize remote stores explicitly, with Development Preview initialization outside builds.

GitHub remains the canonical remote. Readonly GitHub Actions checks and the Development-first source parity gate precede release. Railway source configuration has been retired; hosted resources remain frozen pending the separate operator retirement step. Historical backups and encrypted objects are recovery material, not a running fallback writer.

## Local Development and Recovery

`cmd/appview` runs the combined backend with local SQLite. Portable Web and AppView Docker images, the persistent-volume guard, non-root entrypoint, verified SQLite backup and offline preflight commands remain supported. No local Postgres/Redis stack or retired Swift backend is required.

See the [runtime guide](standalone-reader.md) for configuration, schema initialization and recovery, and [test plans](../test-plans/README.md) for checks. The old [distributed AppView](appview.md), [indexing](indexing-services.md) and [Redis](redis.md) designs are historical references.
