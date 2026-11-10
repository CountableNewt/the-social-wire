import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, mock, spyOn } from "bun:test";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import * as Navigation from "next/navigation";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import * as Auth from "@/hooks/useAuth";
import * as Publishing from "@/lib/articles/articlePublishingClient";
import * as Storage from "@/lib/articles/articleDraftStorage";
import * as Sync from "@/lib/articles/articleDraftSync";
import type { ArticleDraft } from "@/lib/articles/articleDraftTypes";
import type { ArticlePublication } from "@/lib/articles/articlePublishingTypes";
import { SendArticleToDraftDialog } from "@/components/Articles/SendArticleToDraftDialog";
import { createArticleDraftFromLink } from "@/lib/articles/createArticleDraftFromLink";

const did = "did:plc:abcdefghijklmnopqrstuvwx";
const publication: ArticlePublication = { uri: `at://${did}/site.standard.publication/first`, cid: "first", name: "First Publication", url: "https://blog.test", host: "unknown", record: {} };
const second = { ...publication, uri: `at://${did}/site.standard.publication/second`, name: "Second Publication" };
const pckt = { ...publication, uri: `at://${did}/site.standard.publication/pckt`, name: "pckt Blog", host: "pckt" as const };
let viewer = did;
let oauth: OAuthSession;
let epoch = 0;
let client: QueryClient;
let saved: ArticleDraft | undefined;
const push = mock((path: string) => { void path; });
const closes = mock(() => {});
const restores: (() => void)[] = [];
const originalRAF = Object.getOwnPropertyDescriptor(globalThis, "requestAnimationFrame");
const originalCancelRAF = Object.getOwnPropertyDescriptor(globalThis, "cancelAnimationFrame");
beforeAll(() => {
  Object.defineProperty(globalThis, "requestAnimationFrame", { configurable: true, value: (callback: FrameRequestCallback) => Number(setTimeout(() => callback(performance.now()), 0)) });
  Object.defineProperty(globalThis, "cancelAnimationFrame", { configurable: true, value: (id: number) => clearTimeout(id) });
});
afterAll(() => {
  if (originalRAF) Object.defineProperty(globalThis, "requestAnimationFrame", originalRAF); else Reflect.deleteProperty(globalThis, "requestAnimationFrame");
  if (originalCancelRAF) Object.defineProperty(globalThis, "cancelAnimationFrame", originalCancelRAF); else Reflect.deleteProperty(globalThis, "cancelAnimationFrame");
});
beforeEach(() => {
  viewer = did; epoch = 0; saved = undefined; push.mockClear(); closes.mockClear();
  oauth = { did } as unknown as OAuthSession;
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  const auth = spyOn(Auth, "useAuth").mockImplementation(() => ({ session: { did: viewer }, oauthSessionReloadSeq: epoch, getOAuthSession: () => { if (oauth.did !== viewer) oauth = { did: viewer } as unknown as OAuthSession; return oauth; } } as ReturnType<typeof Auth.useAuth>));
  const router = spyOn(Navigation, "useRouter").mockReturnValue({ push } as unknown as ReturnType<typeof Navigation.useRouter>);
  const publications = spyOn(Publishing, "listArticlePublications").mockResolvedValue([publication, second, pckt]);
  const get = spyOn(Storage, "getArticleDraft").mockResolvedValue(undefined);
  const save = spyOn(Storage, "saveArticleDraft").mockImplementation(async (_did, draft) => { saved = { ...draft, revision: 1 }; return saved; });
  const sync = spyOn(Sync, "syncArticleDraft").mockImplementation(async () => saved);
  restores.push(...[auth, router, publications, get, save, sync].map(spy => () => spy.mockRestore()));
});
afterEach(() => { cleanup(); client.clear(); restores.splice(0).reverse().forEach(restore => restore()); });
async function show(ownerEntryId?: string) {
  let view!: ReturnType<typeof render>;
  await act(async () => { view = render(<QueryClientProvider client={client}><SendArticleToDraftDialog url="https://publisher.test/article" close={closes} ownerEntryId={ownerEntryId} /></QueryClientProvider>); });
  await screen.findByRole("option", { name: "Second Publication" });
  return view;
}
describe("Send to Draft", () => {
  it("chooses a publication and opens a new private draft containing the article URL", async () => {
    const sourceEntryId = "at://did:plc:author/site.standard.document/source";
    await show(sourceEntryId);
    expect(screen.getByRole("dialog").getAttribute("data-entry-owner")).toBe(sourceEntryId);
    expect(screen.getByRole("button", { name: "Create Draft" }).hasAttribute("disabled")).toBe(true);
    expect((screen.getByRole("option", { name: /pckt Blog/ }) as HTMLOptionElement).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Draft Publication"), { target: { value: second.uri } });
    fireEvent.click(screen.getByRole("button", { name: "Create Draft" }));
    await waitFor(() => expect(push).toHaveBeenCalledTimes(1));
    expect(saved).toMatchObject({ title: "", markdown: "https://publisher.test/article\n\n", publicationUri: second.uri, assets: [] });
    expect(saved?.publishedUri).toBeUndefined();
    expect(push).toHaveBeenCalledWith(`/articles?draft=${saved!.id}`);
    expect(Sync.syncArticleDraft).toHaveBeenCalledTimes(1);
  });
  it("cancels without creating any draft", async () => {
    await show();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(closes).toHaveBeenCalledTimes(1);
    expect(Storage.saveArticleDraft).not.toHaveBeenCalled();
    expect(Sync.syncArticleDraft).not.toHaveBeenCalled();
    expect(push).not.toHaveBeenCalled();
  });
  it("retains a locally saved draft after failed sync and retries the same draft", async () => {
    const sync = spyOn(Sync, "syncArticleDraft").mockRejectedValueOnce(new Error("Offline")).mockImplementation(async () => saved); restores.push(() => sync.mockRestore());
    await show();
    fireEvent.change(screen.getByLabelText("Draft Publication"), { target: { value: publication.uri } });
    fireEvent.click(screen.getByRole("button", { name: "Create Draft" }));
    await screen.findByText("Offline");
    const id = saved!.id;
    expect(screen.getByRole("button", { name: "Open Draft" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry Private Sync" }));
    await waitFor(() => expect(push).toHaveBeenCalledWith(`/articles?draft=${id}`));
    expect(Storage.saveArticleDraft).toHaveBeenCalledTimes(1);
    expect(sync.mock.calls.map(args => args[1])).toEqual([id, id]);
  });
  it("can open a saved draft even when private sync is unavailable", async () => {
    const sync = spyOn(Sync, "syncArticleDraft").mockRejectedValue(new Error("Offline")); restores.push(() => sync.mockRestore());
    await show();
    fireEvent.change(screen.getByLabelText("Draft Publication"), { target: { value: publication.uri } });
    fireEvent.click(screen.getByRole("button", { name: "Create Draft" }));
    await screen.findByText("Offline");
    fireEvent.click(screen.getByRole("button", { name: "Open Draft" }));
    expect(push).toHaveBeenCalledWith(`/articles?draft=${saved!.id}`);
    expect(Storage.saveArticleDraft).toHaveBeenCalledTimes(1);
  });
  it("admits only one submission while sync is pending and fences an account change", async () => {
    let finish!: (draft: ArticleDraft | undefined) => void;
    const sync = spyOn(Sync, "syncArticleDraft").mockImplementation(() => new Promise(resolve => { finish = resolve; })); restores.push(() => sync.mockRestore());
    await show();
    fireEvent.change(screen.getByLabelText("Draft Publication"), { target: { value: publication.uri } });
    fireEvent.click(screen.getByRole("button", { name: "Create Draft" }));
    await waitFor(() => expect(sync).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole("button", { name: "Creating Draft…" }));
    viewer = "did:plc:different";
    await act(async () => { finish(saved); });
    expect(push).not.toHaveBeenCalled();
    expect(Storage.saveArticleDraft).toHaveBeenCalledTimes(1);
  });
  it("rejects unsupported publications, foreign ownership, and unsafe links before saving", async () => {
    const actor = { viewerDid: did, session: { did } } as unknown as Sync.ArticleDraftActor;
    await expect(createArticleDraftFromLink(actor, pckt, "https://article.test")).rejects.toThrow("pckt");
    await expect(createArticleDraftFromLink(actor, { ...publication, uri: "at://did:plc:other/site.standard.publication/first" }, "https://article.test")).rejects.toThrow("Your Publications");
    for (const url of ["javascript:alert(1)", "https://user:pass@article.test", "http://article.test"]) await expect(createArticleDraftFromLink(actor, publication, url)).rejects.toThrow();
    expect(Storage.saveArticleDraft).not.toHaveBeenCalled();
  });
  it("does not overwrite a pre-existing draft", async () => {
    const existing = spyOn(Storage, "getArticleDraft").mockResolvedValue({ id: "existing" } as ArticleDraft); restores.push(() => existing.mockRestore());
    const actor = { viewerDid: did, session: { did } } as unknown as Sync.ArticleDraftActor;
    await expect(createArticleDraftFromLink(actor, publication, "https://article.test")).rejects.toThrow("Already Exists");
    expect(Storage.saveArticleDraft).not.toHaveBeenCalled();
  });
  it("rejects a stale same-account authorization after private sync completes", async () => {
    let finish!: (draft: ArticleDraft | undefined) => void;
    const sync = spyOn(Sync, "syncArticleDraft").mockImplementation(() => new Promise(resolve => { finish = resolve; })); restores.push(() => sync.mockRestore());
    const view = await show();
    fireEvent.change(screen.getByLabelText("Draft Publication"), { target: { value: publication.uri } });
    fireEvent.click(screen.getByRole("button", { name: "Create Draft" }));
    await waitFor(() => expect(sync).toHaveBeenCalledTimes(1));
    epoch++;
    await act(async () => { view.rerender(<QueryClientProvider client={client}><SendArticleToDraftDialog url="https://publisher.test/article" close={closes} /></QueryClientProvider>); });
    await act(async () => { finish(saved); });
    expect(push).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toContain("Your Account Changed");
  });
});
