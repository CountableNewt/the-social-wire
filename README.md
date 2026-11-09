# The Social Wire

A reader for the [standard.site](https://standard.site) publishing ecosystem, built on ATProto.

## Overview

The Social Wire lets you read publications from people you follow on Bluesky and the broader ATProto network. Your reading preferences — folders, publication organisation — are stored on your own ATProto PDS, not on our servers.

```
Web Frontend (Next.js)       iOS/iPadOS (SwiftUI)
          │                         │
          └────── ATProto OAuth ────┘
                         │
       User's PDS        │       Swift AppView (Railway)
       User-owned        └────── OAuth/DPoP + Reader API
       protocol records          SQLite on persistent /data
                                 Bounded RSS + bundled media processor
```

The hosted target has two services: Frontend and AppView with SQLite. AppView
retains the public XRPC and `/v1/*` compatibility contracts on the existing API
domains. See the [standalone cutover runbook](docs/architecture/standalone-reader.md)
for data import, key preservation, backups, and Development-first acceptance.

GitHub remains origin, GitHub Actions runs required verification, and Railway
builds and deploys the protected branches through its GitHub integration.

## Monorepo Structure

```
the-social-wire/
  apps/
    web/             # Next.js Frontend; standalone Node runtime
    apple/           # SwiftUI iOS/iPadOS app
    operations/      # Next.js operator console (Bun)
  services/
    gateway/         # Distributed rollback source
    appview/         # Public Swift API, SQLite, RSS and bundled media
    jetstream-ingest/ # Retired ingress source and historical contracts
    indexing-worker/ # Retained distributed worker source
    operations/      # Operations control plane (Railway)
  packages/
    lexicons/        # record schemas plus app.thesocialwire.* service XRPC lexicons
    spec/            # OpenAPI 3.1 compatibility contract + endpoint manifest
    swift/           # Read-state and vendored reader core; contract fixtures
  database/
    migrations/      # Historical Postgres schema and import source
  docs/
    architecture/
    wiki/        # Canonical public wiki Markdown (GitHub sync; manual Lichen publish)
```

## Prerequisites

| Tool | Version |
|------|---------|
| [Bun](https://bun.sh) | Matches root [`package.json`](package.json) `packageManager` (currently 1.3.x) |
| [Swift](https://swift.org/install) | 6.4.2 for active backend services |
| [Go](https://go.dev/dl/) | 1.26.6 for retained Go parity suites and excluded services |
| [Railway CLI](https://docs.railway.com/guides/cli) | Latest (hosted operations and migration access) |
| [Xcode](https://developer.apple.com/xcode/) | 16+ (for iOS) |

## Quick Start

### Local development

```bash
# 1. Install JS dependencies
bun install

# 2. Start the web app
cd apps/web
cp .env.example .env.local
# Optional: uncomment or set vars in .env.local (defaults work for local OAuth loopback)
bun run dev
```

Open [http://localhost:3000](http://localhost:3000).

### Full-stack local dev (optional)

The service executables currently accept only `APP_ENV=dev|prod` because their
shared Operations namespace rejects `local`. Use an isolated disposable Postgres
database for local integration; never point these commands at a hosted
Development or Production database.

```bash
# Apply migrations to an isolated disposable Postgres database first.
DATABASE_URL='postgresql://…' bash scripts/apply-database-migrations.sh

# Run each service in a separate terminal with the same disposable DATABASE_URL.
# Gateway (OAuth, sync, writes)
(cd services/gateway && APP_ENV=dev DATABASE_URL='postgresql://…' APPVIEW_BASE_URL=http://127.0.0.1:8081 GATEWAY_APPVIEW_INTERNAL_SECRET=local-development-only PDS_ATTESTATION_RECEIPT_SECRET=local-development-attestation-receipt-secret swift run Gateway)

# AppView (sidebar + Thin AppView reads)
(cd services/appview && APP_ENV=dev DATABASE_URL='postgresql://…' ENABLE_THIN_APPVIEW=true GATEWAY_APPVIEW_INTERNAL_SECRET=local-development-only swift run AppView)

# Replicated projection role (active AppView durable inbox)
(cd services/indexing-worker && APP_ENV=dev DATABASE_URL='postgresql://…' ENABLE_THIN_APPVIEW=true INDEXING_WORKER_ROLE=projection THIN_APPVIEW_JETSTREAM_MODE=v2_authoritative swift run IndexingWorker)

# Coordinator uses the same package with INDEXING_WORKER_ROLE=coordinator.
# Ingress Controller configuration/protocol details: services/jetstream-ingest/README.md
```

### Running tests

See **[docs/test-plans/README.md](docs/test-plans/README.md)** for per-surface plans and PR checklists.

```bash
(cd apps/web && bun test)
(cd apps/operations && bun test)
(cd packages/swift/GatewayCore && swift test)
(cd packages/swift/SocialWireRedis && swift test)
(cd services/gateway && swift test)
(cd services/appview && swift test)
(cd packages/swift/ThinAppViewCore && swift test)
(cd services/indexing-worker && swift test --force-resolved-versions)
(cd packages/swift/OperationsCore && swift test)
(cd services/jetstream-ingest && swift test --force-resolved-versions)

# iOS — Cmd+U in Xcode (see docs/test-plans/apple.md)
```

## Architecture Principles

- **Protocol-first where data is portable**: folders, publication preferences, subscriptions, and read-later records live on the user's own ATProto PDS
- **AppView-owned read state**: feed read/unread state is local-first in clients and synchronized to Social Wire AppView for counters and unread filtering
- **Thin AppView read path**: signed-in clients load bootstrap data, feeds, and entry detail through the public AppView; standard.site bodies remain authoritative on publisher PDSes, while RSS feed bodies may be retained in the derived index (see [docs/architecture/appview.md](docs/architecture/appview.md))
- **Local projections**: AppView keeps reader projections, private drafts, analytics, podcasts and caches in SQLite. User-authored protocol records remain authoritative on the PDS.
- **Direct ATProto where it fits**: discovery and repo reads use public XRPC; Bluesky App View (`public.api.bsky.app`) for follows and profiles only
- **Interoperable by design**: lexicons are public — any ATProto client can read a user's Social Wire folders

## Deployment

| Component | Where |
|-----------|-------|
| Frontend | Railway, `apps/web/Dockerfile` |
| AppView + SQLite + bundled media processor | Railway, `services/appview/Dockerfile.standalone`, one `/data` volume |
| Required CI | GitHub Actions, packaged Frontend and SQLite AppView checks |
| Releases | Railway GitHub integration, Development before Production |

Select `/railway/web-standalone.json` and `/railway/appview-standalone.json` for
the two services. Historical distributed sources and Postgres backups remain
available during the rollback window. Follow the [cutover runbook](docs/architecture/standalone-reader.md)
before retiring old compute; preserve databases, data volumes, object buckets and keys.

## Docs

- **[Test plans](docs/test-plans/README.md)** — verification commands and coverage inventory
- **[Contributing](CONTRIBUTING.md)** — PR workflow and test location conventions
- **[GitHub Wiki](https://github.com/Stygian-Tech/the-social-wire/wiki)** — curated navigation and links into this repository
- **[Lichen Wiki](https://lichen.wiki/@samclemente.me/the-social-wire)** — public user and developer documentation; publish from `docs/wiki/`
- [Architecture overview](docs/architecture/overview.md)
- [Lexicons](docs/architecture/lexicons.md)
- [Discovery chain](docs/architecture/discovery.md)
- [Thin AppView](docs/architecture/appview.md)
- [Replicated indexing services](docs/architecture/indexing-services.md)
- [Redis cache and coordination](docs/architecture/redis.md)
- [Web app](apps/web/README.md)
- [Apple app](apps/apple/README.md)
- [OpenAPI spec](packages/spec/README.md)
- [Lexicon reference](packages/lexicons/README.md)

## License

This project is licensed under the MIT License — see [LICENSE](LICENSE).
