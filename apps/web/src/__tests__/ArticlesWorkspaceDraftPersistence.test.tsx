import { afterAll, afterEach, beforeAll, beforeEach, expect, it, spyOn } from "bun:test";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { IDBFactory } from "fake-indexeddb";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import * as Auth from "@/hooks/useAuth";
import * as Semble from "@/hooks/useSembleReadLater";
import * as Cloud from "@/lib/articles/articleDraftCloudClient";
import type { ArticleDraftActor, ArticleDraftDocument } from "@/lib/articles/articleDraftCloudTypes";
import * as Sync from "@/lib/articles/articleDraftSync";
import * as Publishing from "@/lib/articles/articlePublishingClient";
import { getArticleDraft, listArticleDrafts, saveArticleDraft, getArticleDraftSyncState } from "@/lib/articles/articleDraftStorage";
import { ArticlesWorkspace } from "@/components/Articles/ArticlesWorkspace";
import { SidebarProvider } from "@/components/ui/sidebar";
const realSyncOne = Sync.syncArticleDraft, realSyncAll = Sync.syncArticleDrafts, realLoad = Sync.loadSyncedArticleDraft;
function abortable<T>(context: ArticleDraftActor, promise: Promise<T>): Promise<T> {
  if (!context.signal) return promise;
  const signal = context.signal;
  return new Promise((resolve, reject) => {
    const abort = () => reject(signal.reason);
    if (signal.aborted) { abort(); return; }
    signal.addEventListener("abort", abort, { once: true });
    promise.then(value => { signal.removeEventListener("abort", abort); resolve(value); }, error => { signal.removeEventListener("abort", abort); reject(error); });
  });
}
const did = "did:plc:abcdefghijklmnopqrstuvwx";
let client: QueryClient;
const restores: (() => void)[] = [];
const originalMatchMedia = Object.getOwnPropertyDescriptor(window, "matchMedia");
const originalIDB = Object.getOwnPropertyDescriptor(globalThis, "indexedDB");
beforeAll(() => { Object.defineProperty(window, "matchMedia", { configurable: true, value: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) }); });
afterAll(() => { if (originalMatchMedia) Object.defineProperty(window, "matchMedia", originalMatchMedia); else Reflect.deleteProperty(window, "matchMedia"); });
beforeEach(() => {
  Object.defineProperty(globalThis, "indexedDB", { configurable: true, value: new IDBFactory() });
  const sync = spyOn(Sync, "syncArticleDraft").mockImplementation(async (actor, id) => getArticleDraft(actor.viewerDid, id)); restores.push(() => sync.mockRestore());
  const all = spyOn(Sync, "syncArticleDrafts").mockImplementation(async actor => listArticleDrafts(actor.viewerDid)); restores.push(() => all.mockRestore());
  const load = spyOn(Sync, "loadSyncedArticleDraft").mockImplementation(async (actor, id) => getArticleDraft(actor.viewerDid, id)); restores.push(() => load.mockRestore());
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  const research = spyOn(Semble, "useSembleCollections").mockReturnValue({ collections: [], isPending: false, error: null, hasNextPage: false } as unknown as ReturnType<typeof Semble.useSembleCollections>); restores.push(() => research.mockRestore());
  const auth = spyOn(Auth, "useAuth").mockReturnValue({ session: { did }, getOAuthSession: () => ({ did } as unknown as OAuthSession), oauthSessionReloadSeq: 0 } as ReturnType<typeof Auth.useAuth>); restores.push(() => auth.mockRestore());
  const pubs = spyOn(Publishing, "listArticlePublications").mockResolvedValue([{ uri: `at://${did}/site.standard.publication/one`, cid: "pub", name: "Test Publication", url: "https://publication.test", host: "leaflet", record: {} }]); restores.push(() => pubs.mockRestore());
  const history = spyOn(Publishing, "listPublishedArticles").mockResolvedValue([]); restores.push(() => history.mockRestore());
});
afterEach(() => { cleanup(); client.clear(); restores.splice(0).reverse().forEach(restore => restore()); if (originalIDB) Object.defineProperty(globalThis, "indexedDB", originalIDB); else Reflect.deleteProperty(globalThis, "indexedDB"); });
it("keeps a newly entered title through immediate mode changes, body edits, Save Draft, and Article Viewer", async () => {
  await act(async () => { render(<QueryClientProvider client={client}><SidebarProvider><ArticlesWorkspace /></SidebarProvider></QueryClientProvider>); });
  await waitFor(() => expect((screen.getByRole("button", { name: "New Article" }) as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(screen.getByRole("button", { name: "New Article" }));
  const title = await screen.findByLabelText("Article Title");
  fireEvent.change(title, { target: { value: "Retained New Article Title" } });
  fireEvent.click(screen.getByRole("button", { name: "Markdown" }));
  fireEvent.change(screen.getByLabelText("Article Markdown"), { target: { value: "# A New Body\n\nSaved Article Content." } });
  fireEvent.click(screen.getByRole("button", { name: "Save Draft" }));
  await waitFor(async () => {
    const rows = await listArticleDrafts(did);
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ title: "Retained New Article Title", markdown: "# A New Body\n\nSaved Article Content." });
  });
  await waitFor(() => expect((screen.getByRole("button", { name: "Save Draft" }) as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(screen.getByRole("button", { name: "Article Viewer" }));
  expect(screen.getByRole("heading", { name: "Retained New Article Title" })).toBeTruthy();
  expect(screen.getByText("Saved Article Content.")).toBeTruthy();
  expect(screen.queryByRole("region", { name: "Article Library" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Back to Articles" }));
  expect(await screen.findByRole("button", { name: /^Retained New Article Title/ })).toBeTruthy();
  cleanup(); client.clear();
  await act(async () => { render(<QueryClientProvider client={client}><SidebarProvider><ArticlesWorkspace /></SidebarProvider></QueryClientProvider>); });
  expect(screen.getByRole("button", { name: "Research" }).getAttribute("aria-pressed")).toBe("true");
  expect(screen.queryByLabelText("Article Title")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Drafts" }));
  fireEvent.click(await screen.findByRole("button", { name: /^Retained New Article Title/ }));
  const restored = await screen.findByLabelText("Article Title");
  expect((restored as HTMLInputElement).value).toBe("Retained New Article Title");
  fireEvent.click(screen.getByRole("button", { name: "Markdown" }));
  expect((screen.getByLabelText("Article Markdown") as HTMLTextAreaElement).value).toBe("# A New Body\n\nSaved Article Content.");
});


it("opens a persisted draft from its library row while cloud requests are stalled and preserves new edits after late readback", async () => {
  const cached = await saveArticleDraft(did, { id: "11111111-1111-4111-8111-111111111111", title: "Cached Article", markdown: "# Cached Body\n\nPrivate cached content.", excerpt: "", path: "/cached-article", tags: [], publicationUri: `at://${did}/site.standard.publication/one`, createdAt: "2026-11-01T00:00:00Z", updatedAt: "2026-11-01T00:00:00Z", assets: [] });
  const never = new Promise<never>(() => {});
  let finish!: (value: ArticleDraftDocument | undefined) => void;
  const reply = new Promise<ArticleDraftDocument | undefined>(resolve => { finish = resolve; });
  const all = spyOn(Sync, "syncArticleDrafts").mockImplementation(realSyncAll); restores.push(() => all.mockRestore());
  const one = spyOn(Sync, "syncArticleDraft").mockImplementation(realSyncOne); restores.push(() => one.mockRestore());
  const load = spyOn(Sync, "loadSyncedArticleDraft").mockImplementation(realLoad); restores.push(() => load.mockRestore());
  const list = spyOn(Cloud, "listCloudArticleDrafts").mockImplementation(context => abortable(context, never)); restores.push(() => list.mockRestore());
  const get = spyOn(Cloud, "getCloudArticleDraft").mockImplementation(context => abortable(context, reply)); restores.push(() => get.mockRestore());
  const put = spyOn(Cloud, "putCloudArticleDraft").mockImplementation(async (_actor, input) => ({ draftId: input.draft.id, revision: input.expectedRevision + 1, mutationId: input.mutationId, updatedAt: input.draft.updatedAt, deleted: false, draft: input.draft, attachments: [] })); restores.push(() => put.mockRestore());
  await act(async () => { render(<QueryClientProvider client={client}><SidebarProvider><ArticlesWorkspace /></SidebarProvider></QueryClientProvider>); });
  fireEvent.click(screen.getByRole("button", { name: "Drafts" }));
  fireEvent.click(await screen.findByRole("button", { name: /^Cached Article/ }));
  const title = await screen.findByLabelText("Article Title");
  expect((title as HTMLInputElement).value).toBe("Cached Article");
  expect((title as HTMLInputElement).disabled).toBe(false);
  expect(screen.getByRole("complementary", { name: "Article Details" })).toBeTruthy();
  fireEvent.change(title, { target: { value: "Edited Without Waiting for Cloud" } });
  await waitFor(async () => expect((await getArticleDraft(did, cached.id))?.title).toBe("Edited Without Waiting for Cloud"));
  const { revision: _revision, ...payload } = cached; void _revision;
  await act(async () => { finish({ draftId: cached.id, revision: 1, mutationId: crypto.randomUUID(), updatedAt: cached.updatedAt, deleted: false, draft: { ...payload, assets: payload.assets ?? [], title: "Stale Cloud Title" }, attachments: [] }); });
  await waitFor(async () => expect((await getArticleDraftSyncState(did, cached.id))?.serverRevision).toBe(2));
  expect((screen.getByLabelText("Article Title") as HTMLInputElement).value).toBe("Edited Without Waiting for Cloud");
  expect((await getArticleDraft(did, cached.id))?.title).toBe("Edited Without Waiting for Cloud");
});
