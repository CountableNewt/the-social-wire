# Contributing

Read the full [CONTRIBUTING.md](https://github.com/Stygian-Tech/the-social-wire/blob/main/CONTRIBUTING.md) and the repository `AGENTS.md` before changing code.

## Clone and install

```bash
git clone https://github.com/Stygian-Tech/the-social-wire.git
cd the-social-wire
bun install
```

Never work directly on `main`. If the checkout is already on an issue branch, keep using it. Otherwise create a focused `feature/`, `fix/`, `chore/`, `refactor/`, or `test/` branch.

## Choose a surface

| Area | Reference |
|------|-----------|
| Web | [apps/web/README.md](https://github.com/Stygian-Tech/the-social-wire/blob/main/apps/web/README.md) |
| Apple | [apps/apple/README.md](https://github.com/Stygian-Tech/the-social-wire/blob/main/apps/apple/README.md) |
| AppView | `services/appview` |

The backend is Go AppView with `APP_ENV=dev|prod` and an absolute SQLite path. Use disposable local databases and the required scheduling/draft configuration; never point experiments at hosted data. Native shared Swift code lives in `apps/apple/Packages/ReadStateCore`.

## Working expectations

- Keep diffs focused and follow existing architecture.
- Add or update tests in the owning package.
- Add matching Bruno requests and OpenAPI coverage when routes change.
- Run type checks, lint, builds, and tests appropriate to the affected surfaces.
- Do not commit secrets or real environment files.
- Do not commit, push, open a PR, or deploy unless that action is explicitly requested.

See [[Testing]] for commands and CI mapping.

## Wiki source and publishing

`docs/wiki/` is the canonical Markdown source. Internal links use
Lichen-compatible double-bracket wiki-link syntax.

On `main`, `.github/workflows/publish-wiki.yml` mirrors the directory to the GitHub Wiki. It does **not** publish to Lichen. The public [Lichen wiki](https://lichen.wiki/@samclemente.me/the-social-wire) must currently be updated separately:

1. Replace Lichen's Home note with `docs/wiki/Home.md`.
2. Create or update one Lichen note for each other public Markdown file in `docs/wiki/`.
3. Preserve the page filename/slug so checked-in internal wiki links resolve.
4. Do not import `_Sidebar.md`; it is GitHub Wiki navigation metadata, while Home is the curated Lichen index.
5. Verify the Lichen Pages list, links, tables, code fences, and the signed-out view after publishing.

Edits made only in the GitHub Wiki or Lichen are not mirrored back to the repository and may drift. Make the source change here first.

Related: [[Monorepo-map]], [[Testing]], [[Deployment-and-environments]].
