# The Social Wire

A reader for the [standard.site](https://standard.site) publishing ecosystem, built on ATProto.

## Overview

The Social Wire lets you read publications from people you follow on Bluesky and the broader ATProto network. Your reading preferences — folders, publication organisation — are stored on your own ATProto PDS, not on our servers.

```
Web Frontend (Next.js)       iOS/iPadOS (SwiftUI)
          │                         │
          └────── ATProto OAuth ────┘
                         │
       User's PDS        │       Go AppView (Vercel)
       User-owned        └────── OAuth/DPoP + Reader API
       protocol records          Turso + private encrypted Blob storage
                                 Bounded RSS + leased scheduling
```

The hosted target has two Vercel projects: Web and AppView. AppView
retains the public XRPC and `/v1/*` compatibility contracts on the existing API
domains. See the [standalone cutover runbook](docs/architecture/standalone-reader.md)
for data import, key preservation, backups, and Development-first acceptance.

GitHub remains origin and GitHub Actions runs required verification. Both Vercel
projects use `main` for Production and the `dev` branch Preview for Development.
Issue #963 records the hosted cutover and separate Railway infrastructure retirement.

## Monorepo Structure

```
the-social-wire/
  apps/
    web/             # Next.js Frontend; Vercel and portable Node runtime
    apple/           # SwiftUI client; shared ReadStateCore in Packages/
  services/
    appview/         # Public Go API, Turso/SQLite, RSS and scheduling
  packages/
    lexicons/        # record schemas plus app.thesocialwire.* service XRPC lexicons
    spec/            # OpenAPI 3.1 compatibility contract + endpoint manifest
    read-state/      # Web read-state protocol implementation
  docs/
    architecture/
    wiki/        # Canonical public wiki Markdown (GitHub sync; manual Lichen publish)
```

## Prerequisites

| Tool | Version |
|------|---------|
| [Bun](https://bun.sh) | Matches root [`package.json`](package.json) `packageManager` (currently 1.3.x) |
| [Go](https://go.dev/dl/) | Matches [AppView go.mod](services/appview/go.mod) |
| [Swift](https://swift.org/install) | Supplied by Xcode for the native client |
| [Vercel CLI](https://vercel.com/docs/cli) and [Turso CLI](https://docs.turso.tech/cli/introduction) | Hosted operations only; not needed for local development |
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

Run the standalone Go backend with a disposable SQLite database. The Operations
store uses the `dev` environment for local development.

```bash
cd services/appview
APP_ENV=dev BACKEND_MODE=standalone \
  SQLITE_DB_PATH="$(mktemp -d)/social-wire.sqlite" \
  BIND_HOST=127.0.0.1 \
  SCHEDULING_PUBLIC_ORIGIN=https://api.testing.thesocialwire.app \
  SCHEDULING_STORAGE_KEY="$(openssl rand -base64 32)" \
  PDS_ATTESTATION_RECEIPT_SECRET=local-development-attestation-receipt-secret \
  go run ./cmd/appview --port 8081
```

Set `NEXT_PUBLIC_SOCIALWIRE_API_URL=http://localhost:8081` in the local Web
environment before starting the Web app.

### Running tests

See **[docs/test-plans/README.md](docs/test-plans/README.md)** for per-surface plans and PR checklists.

```bash
(cd apps/web && bun test)
(cd services/appview && go test -vet=all -count=1 ./...)
node --test scripts/ci/*.test.mjs
bash scripts/ci/verify-runtime-images.sh appview

# iOS — Cmd+U in Xcode (see docs/test-plans/apple.md)
```

## Architecture Principles

- **Protocol-first where data is portable**: folders, publication preferences, subscriptions, and read-later records live on the user's own ATProto PDS
- **AppView-owned read state**: feed read/unread state is local-first in clients and synchronized to Social Wire AppView for counters and unread filtering
- **Thin AppView read path**: signed-in clients load bootstrap data, feeds, and entry detail through the public AppView; standard.site bodies remain authoritative on publisher PDSes, while RSS feed bodies may be retained in the derived index (see [docs/architecture/appview.md](docs/architecture/appview.md))
- **Server projections**: AppView keeps reader projections, private drafts, analytics and caches in Turso, with SQLite available locally. The selected migration target is classic libSQL over the existing official pure-Go HTTP driver (`turso.tech/database/tursogo-serverless`), with `DATABASE_BACKEND=turso` and bare `libsql://`/`turso://`/`https://` URLs normalized to HTTPS. Source configuration does not establish hosted cutover; Development acceptance must precede Production. User-authored protocol records remain authoritative on the PDS. Private attachment ciphertext lives in private Blob storage.
- **Direct ATProto where it fits**: discovery and repo reads use public XRPC; Bluesky App View (`public.api.bsky.app`) for follows and profiles only
- **Interoperable by design**: lexicons are public — any ATProto client can read a user's Social Wire folders

## Deployment

| Component | Where |
|-----------|-------|
| Frontend | Vercel Web project, root `apps/web` |
| AppView | Vercel backend project, root `services/appview`, six Go Functions |
| Data | Separate US Turso AppView/scheduling databases and private Blob storage per environment |
| Required CI | GitHub Actions, Web/Go/Apple/source contracts and portable image checks |
| Releases | `dev` branch Preview before `main` Production in both projects |

Follow the [runtime and cutover guide](docs/architecture/standalone-reader.md)
for explicit schema initialization, stable domains and recovery. Railway resource
retirement requires a separate operator step; source cleanup does not delete infrastructure.
The Dockerfiles remain available for local verification and recovery.

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
