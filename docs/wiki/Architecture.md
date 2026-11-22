# Architecture

The Social Wire separates portable user records from application projections and private unpublished state.

| Layer | Responsibility |
|-------|----------------|
| **Web and Apple clients** | ATProto sign-in, direct PDS publishing, local cache and reader UI |
| **Go AppView** | OAuth/DPoP edge, publications, feeds, unread state, bootstrap stream, drafts, analytics, scheduling and integrations |
| **Turso** | Distinct AppView and scheduling databases per environment, in the United States |
| **Private Blob storage** | Server-encrypted draft attachment ciphertext |

The viewer's PDS remains authoritative for user-authored protocol records; publisher PDSes remain authoritative for standard.site records. Server projection rules are shared by Web and Apple clients. Initial reader bootstrap streams NDJSON without buffering.

Root source configuration targets one retained Vercel Services project with Web and six Go AppView services, using `main` for Production and the `dev` branch Preview for Development, with separate environment credentials and stores. The backend has six Go Functions, including a leased maintenance dispatcher. Remote startup checks readiness without applying schema DDL; Preview database initialization is an explicit operator action outside builds.

Local SQLite and portable Docker images remain available for development and recovery. The old Railway writer is frozen pending operator infrastructure retirement. Retired distributed Swift, Postgres and Redis designs remain historical references; source cleanup does not delete hosted resources.

See the [current architecture overview](https://github.com/CountableNewt/the-social-wire/blob/main/docs/architecture/overview.md), [runtime guide](https://github.com/CountableNewt/the-social-wire/blob/main/docs/architecture/standalone-reader.md), and [test plans](https://github.com/CountableNewt/the-social-wire/blob/main/docs/test-plans/README.md).

Issue #992 tracks hosted consolidation acceptance. Generic previews remain disabled until isolated resources are provisioned.
