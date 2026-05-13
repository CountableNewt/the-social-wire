## Learned User Preferences

- When asked to commit and push, stage only files changed for the current task, exclude unrelated edits, use a short conventional commit message, and push the active branch (often `main`).
- Prefer focused diffs: match existing project style, keep imports at the top, and avoid drive-by refactors or unsolicited docs.

## Learned Workspace Facts

- The Next.js app lives in `apps/web`; common checks are `bun run turbo run build --filter=web...` and targeted `bun test` under `apps/web`.
- ATProto OAuth uses `atproto` scope: `useAuth().session` is a lightweight `{ did }` shape; anything that needs `fetchHandler` / OAuth on the wire must use `getOAuthSession()` (`OAuthSession`).
- OAuth access tokens here are typically audience-bound to the user’s PDS, not the Bluesky App View—use a plain `Agent('https://bsky.social')` without OAuth-backed `fetch` for public App View reads (e.g. follows, `app.bsky.actor.getProfile`), and use a session-backed `Agent` / `createOAuthAgent` for `com.atproto.repo.*` and other PDS-targeted calls; do not attach the OAuth token to arbitrary `bsky.social` XRPC.
- When bridging `session.fetchHandler` into `@atproto/api`’s `Agent`, satisfy `typeof fetch` (including static `preconnect`) by delegating `preconnect` from global `fetch` on the wrapper.
- standard.site discovery should cover `site.standard.document` and `site.standard.publication`, and retain `site.standard.entry` for backward compatibility; `services/api`’s Swift `DiscoveryChain.swift` reflects the intended multi-collection behavior.
- The repository `.gitignore` includes `.cursor/` so local Cursor metadata is not committed.
