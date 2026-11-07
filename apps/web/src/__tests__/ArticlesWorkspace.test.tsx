import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, mock, spyOn } from "bun:test";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import { ArticleSchedulingNotSubmittedError } from "@/lib/articles/articleSchedulingError";
import * as Scheduling from "@/hooks/useSkejScheduling";
import * as Semble from "@/hooks/useSembleReadLater";
import * as Entitlements from "@/hooks/useProEntitlements";
import * as Auth from "@/hooks/useAuth";
import * as Drafts from "@/hooks/articles/useArticleDrafts";
import * as Storage from "@/lib/articles/articleDraftStorage";
import * as Publishing from "@/lib/articles/articlePublishingClient";
import type { ArticleDraft } from "@/lib/articles/articleDraftTypes";
import type { ArticlePublication, PublishedArticle } from "@/lib/articles/articlePublishingTypes";
import { ArticlesWorkspace } from "@/components/Articles/ArticlesWorkspace";
import { SidebarProvider } from "@/components/ui/sidebar";
const did = "did:plc:abcdefghijklmnopqrstuvwx";
const publications: ArticlePublication[] = [
  { uri: `at://${did}/site.standard.publication/one`, cid: "first", name: "First Publication", url: "https://first.test", host: "leaflet", record: {} },
  { uri: `at://${did}/site.standard.publication/two`, cid: "second", name: "Second Publication", url: "https://second.test", host: "offprint", record: {} },
];
let seed: ArticleDraft;
let storedDraft: ArticleDraft;
let storageError: string | undefined;
let client: QueryClient;
let scheduleRows: unknown[];
let publishedRows: PublishedArticle[];
const scheduleArticle = mock(async (_input: unknown, _time: string): Promise<unknown> => { void _input; void _time; return {}; });
const persist = mock(async () => {});
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
const originalMatchMedia = Object.getOwnPropertyDescriptor(window, "matchMedia");
beforeAll(() => { Object.defineProperty(window, "matchMedia", { configurable: true, value: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) }); });
afterAll(() => { if (originalMatchMedia) Object.defineProperty(window, "matchMedia", originalMatchMedia); else Reflect.deleteProperty(window, "matchMedia"); });
beforeEach(() => {
  seed = { id: "draft", title: "Working Article", markdown: "# Article Body\n\nA private paragraph.", excerpt: "", path: "/working-article", tags: [], publicationUri: publications[0]!.uri, createdAt: "2026-10-10T00:00:00Z", updatedAt: "2026-10-10T00:00:00Z", assets: [], revision: 1 };
  scheduleRows = [];
  publishedRows = [];
  scheduleArticle.mockReset(); scheduleArticle.mockResolvedValue({ scheduleUri: `at://${did}/at.skej.schedule/current` });
  const pro = spyOn(Entitlements, "useProEntitlements").mockReturnValue({ isPro: true } as ReturnType<typeof Entitlements.useProEntitlements>); restores.push(() => pro.mockRestore());
  const schedules = spyOn(Scheduling, "useSkejScheduling").mockReturnValue({ readiness: { data: { connected: true }, refetch: async () => {} }, schedules: { data: scheduleRows, refetch: async () => ({ data: scheduleRows }) }, scheduleArticle, cancel: async () => {}, reschedule: async () => {}, connectUrl: "https://skej.test", pending: false } as unknown as ReturnType<typeof Scheduling.useSkejScheduling>); restores.push(() => schedules.mockRestore());
  storedDraft = seed;
  storageError = undefined; persist.mockReset(); persist.mockResolvedValue(undefined);
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  const research = spyOn(Semble, "useSembleCollections").mockReturnValue({ collections: [], isPending: false, error: null, hasNextPage: false } as unknown as ReturnType<typeof Semble.useSembleCollections>); restores.push(() => research.mockRestore());
  const auth = spyOn(Auth, "useAuth").mockReturnValue({ session: { did }, getOAuthSession: () => ({ did } as unknown as OAuthSession), oauthSessionReloadSeq: 0 } as ReturnType<typeof Auth.useAuth>); restores.push(() => auth.mockRestore());
  const local = spyOn(Drafts, "useArticleDrafts").mockImplementation(() => {
    const [draft, setDraft] = useState(() => { storedDraft = seed; return seed; });
    return { draft, drafts: [draft], loading: false, status: storageError ? "Not Saved" : "Saved in This Browser", error: storageError, update: patch => { storedDraft = { ...storedDraft, ...patch }; setDraft(storedDraft); }, persist, syncing: false, refresh: async () => {}, select: async () => {}, create: async () => draft, remove: async () => {} };
  }); restores.push(() => local.mockRestore());
  const pubs = spyOn(Publishing, "listArticlePublications").mockResolvedValue(publications); restores.push(() => pubs.mockRestore());
  const stored = spyOn(Storage, "getArticleDraft").mockImplementation(async () => storedDraft); restores.push(() => stored.mockRestore());
  const history = spyOn(Publishing, "listPublishedArticles").mockResolvedValue(publishedRows); restores.push(() => history.mockRestore());
});
afterEach(() => { cleanup(); client.clear(); restores.splice(0).reverse().forEach(restore => restore()); });
async function show(openDraft = true) {
  await act(async () => { render(<QueryClientProvider client={client}><SidebarProvider><ArticlesWorkspace /></SidebarProvider></QueryClientProvider>); });
  await screen.findByRole("region", { name: "Article Library" });
  if (openDraft) {
    fireEvent.click(screen.getByRole("button", { name: "Drafts" }));
    fireEvent.click(screen.getByRole("button", { name: new RegExp(`^${seed.title || "Untitled Article"}`) }));
    await waitFor(() => expect(screen.getByRole("option", { name: "First Publication" })).toBeTruthy());
  }
}
function publishingSpy() {
  const publish = spyOn(Publishing, "publishArticle").mockResolvedValue({ uri: `at://${did}/site.standard.document/published`, cid: "published", url: "https://first.test/working-article" }); restores.push(() => publish.mockRestore());
  return publish;
}
describe("article publishing workspace", () => {
  it("keeps Articles and publishing available with scheduling disabled", async () => {
    delete process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING;
    await show(false);
    expect(screen.queryByRole("button", { name: "Scheduled" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Drafts" }));
    fireEvent.click(screen.getByRole("button", { name: /^Working Article/ }));
    expect(await screen.findByLabelText("Article Title")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Publish Article" }).hasAttribute("disabled")).toBe(false);
    expect(screen.queryByRole("button", { name: "Schedule Article" })).toBeNull();
    expect(screen.queryByText("Open Scheduled to Connect Skej.")).toBeNull();
  });
  it("keeps an existing schedule claim locked while scheduling is disabled", async () => {
    delete process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING;
    seed.scheduleRecordKey = "existing-claim"; seed.publicationClaimId = "protected";
    await show();
    expect(screen.getByRole("button", { name: "Publish Article" }).hasAttribute("disabled")).toBe(true);
    expect(screen.getByText(/Draft Remains Locked While Post Scheduling Is Disabled/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Check Schedule" })).toBeNull();
    expect(storedDraft.scheduleRecordKey).toBe("existing-claim");
    expect(storedDraft.publicationClaimId).toBe("protected");
  });
  it("leaves published history idle until its tab opens", async () => {
    const history = spyOn(Publishing, "listPublishedArticles").mockResolvedValue([]); restores.push(() => history.mockRestore());
    await show(false);
    expect(history).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Drafts" }));
    expect(history).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Published" }));
    await waitFor(() => expect(history).toHaveBeenCalledTimes(1));
    expect(await screen.findByText("No Published Articles Yet.")).toBeTruthy();
  });
  it("starts in the full-page library even when a draft was restored", async () => {
    await show(false);
    const sections = within(screen.getByRole("navigation", { name: "Article Sections" })).getAllByRole("button");
    expect(sections.map(button => button.textContent)).toEqual(["Research", "Drafts", "Scheduled", "Published"]);
    expect(screen.getByRole("button", { name: "Research" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: "Drafts" }).getAttribute("aria-pressed")).toBe("false");
    expect(screen.getByRole("region", { name: "Research Library" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /^Working Article/ })).toBeNull();
    expect(screen.queryByLabelText("Article Title")).toBeNull();
    expect(screen.queryByRole("complementary", { name: "Article Details" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Hide Metadata" })).toBeNull();
  });
  it("groups drafts under a publication name and opens the selected row without displaying AT URIs", async () => {
    await show(false);
    fireEvent.click(screen.getByRole("button", { name: "Drafts" }));
    const group = screen.getByRole("region", { name: "First Publication" });
    expect(within(group).getByRole("heading", { name: /^First Publication/ })).toBeTruthy();
    const row = within(group).getByRole("button", { name: /^Working Article/ });
    expect(document.body.textContent).not.toContain("at://");
    fireEvent.click(row);
    expect((await screen.findByLabelText("Article Title") as HTMLInputElement).value).toBe("Working Article");
  });
  it("creates an article from the library and opens editor with metadata", async () => {
    await show(false);
    fireEvent.click(screen.getByRole("button", { name: "New Article" }));
    expect(await screen.findByLabelText("Article Title")).toBeTruthy();
    expect(screen.getByRole("complementary", { name: "Article Details" })).toBeTruthy();
    expect(screen.queryByRole("region", { name: "Article Library" })).toBeNull();
    expect(screen.queryByRole("separator", { name: "Resize Drafts" })).toBeNull();
  });
  it("waits for saving before returning to the library", async () => {
    await show();
    let finish!: () => void;
    persist.mockImplementation(() => new Promise<void>(resolve => { finish = resolve; }));
    fireEvent.change(screen.getByLabelText("Article Title"), { target: { value: "Saved Before Leaving" } });
    fireEvent.click(screen.getByRole("button", { name: "Back to Articles" }));
    expect(screen.getByLabelText("Article Title")).toBeTruthy();
    expect(screen.queryByRole("region", { name: "Article Library" })).toBeNull();
    await act(async () => { finish(); });
    expect(await screen.findByRole("region", { name: "Article Library" })).toBeTruthy();
    expect(screen.getByRole("button", { name: /^Saved Before Leaving/ })).toBeTruthy();
    expect(persist).toHaveBeenCalledTimes(1);
  });
  it("keeps the editor open when saving before leaving fails", async () => {
    await show();
    persist.mockRejectedValue(new Error("Cannot Save Yet"));
    fireEvent.change(screen.getByLabelText("Article Title"), { target: { value: "Keep These Edits" } });
    fireEvent.click(screen.getByRole("button", { name: "Back to Articles" }));
    expect(await screen.findByRole("alert")).toHaveProperty("textContent", "Cannot Save YetRetry");
    expect((screen.getByLabelText("Article Title") as HTMLInputElement).value).toBe("Keep These Edits");
    expect(screen.queryByRole("region", { name: "Article Library" })).toBeNull();
  });
  it("opens published rows read-only with metadata and returns to the Published library", async () => {
    publishedRows.push({ uri: `at://${did}/site.standard.document/public-story`, cid: "published", record: { title: "Public Story", site: publications[0]!.uri, path: "/public-story", content: { $type: "at.markpub.markdown", text: { markdown: "A public paragraph." } } } });
    publishedRows.push({ uri: `at://${did}/site.standard.document/second-story`, cid: "second", record: { title: "Second Story", site: publications[1]!.uri, content: { $type: "at.markpub.markdown", text: { markdown: "Another public paragraph." } } } });
    const publish = publishingSpy();
    await show(false);
    fireEvent.click(screen.getByRole("button", { name: "Published" }));
    expect(within(await screen.findByRole("region", { name: "First Publication" })).getByRole("button", { name: /^Public Story/ })).toBeTruthy();
    expect(within(screen.getByRole("region", { name: "Second Publication" })).getByRole("button", { name: /^Second Story/ })).toBeTruthy();
    expect(document.body.textContent).not.toContain("at://");
    fireEvent.click(await screen.findByRole("button", { name: /^Public Story/ }));
    expect(await screen.findByRole("heading", { name: "Public Story" })).toBeTruthy();
    expect(screen.getByText("A public paragraph.")).toBeTruthy();
    expect(screen.getByRole("complementary", { name: "Article Details" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Open Article" }).getAttribute("href")).toBe("https://first.test/public-story");
    expect(document.body.textContent).not.toContain("at://");
    expect(screen.queryByLabelText("Article Title")).toBeNull();
    expect(screen.queryByRole("button", { name: "Publish Article" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Hide Metadata" }));
    expect(screen.queryByRole("complementary", { name: "Article Details" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Show Metadata" }));
    expect(screen.getByRole("complementary", { name: "Article Details" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Back to Articles" }));
    await screen.findByRole("region", { name: "Article Library" });
    expect(screen.getByRole("button", { name: "Published" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: /^Public Story/ })).toBeTruthy();
    expect(publish).not.toHaveBeenCalled();
  });
  it("opens Scheduled without publishing or losing the active draft", async () => {
    const publish = publishingSpy();
    await show();
    fireEvent.change(screen.getByLabelText("Article Title"), { target: { value: "Still Private" } });
    fireEvent.click(screen.getByRole("button", { name: "Back to Articles" }));
    await screen.findByRole("region", { name: "Article Library" });
    fireEvent.click(screen.getByRole("button", { name: "Scheduled" }));
    expect(screen.getByText("No Scheduled Articles.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Scheduled" }).getAttribute("aria-pressed")).toBe("true");
    expect(publish).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Drafts" }));
    fireEvent.click(screen.getByRole("button", { name: /^Still Private/ }));
    expect((await screen.findByLabelText("Article Title") as HTMLInputElement).value).toBe("Still Private");
    expect(screen.queryByText("No Scheduled Articles.")).toBeNull();
  });
  it("requires public-record confirmation and locks a scheduled snapshot", async () => {
    const publish = publishingSpy();
    await show();
    fireEvent.click(screen.getByRole("button", { name: "Schedule Article" }));
    expect(await screen.findByText(/Skej stores the full article in a public schedule record immediately/)).toBeTruthy();
    expect(scheduleArticle).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("Publish Article At"), { target: { value: "2099-01-01T12:00" } });
    fireEvent.click(screen.getByRole("button", { name: "Confirm Schedule" }));
    await waitFor(() => expect(scheduleArticle).toHaveBeenCalledTimes(1));
    expect(scheduleArticle.mock.calls[0]?.[0]).toMatchObject({ title: seed.title, markdown: seed.markdown });
    await screen.findByRole("region", { name: "Article Library" });
    expect(screen.getByRole("button", { name: "Scheduled" }).getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "Drafts" }));
    fireEvent.click(screen.getByRole("button", { name: /^Working Article/ }));
    await screen.findByLabelText("Article Title");
    expect((screen.getByRole("button", { name: "Publish Article" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByLabelText("Article Title") as HTMLInputElement).disabled).toBe(true);
    expect(persist.mock.calls.length).toBeGreaterThanOrEqual(2);
    expect(publish).not.toHaveBeenCalled();
  });
  it("retains a duplicate-publication lock after an ambiguous scheduling failure", async () => {
    scheduleArticle.mockRejectedValue(new Error("Readback Failed"));
    await show();
    fireEvent.click(screen.getByRole("button", { name: "Schedule Article" }));
    await screen.findByRole("dialog");
    fireEvent.change(screen.getByLabelText("Publish Article At"), { target: { value: "2099-01-01T12:00" } });
    fireEvent.click(screen.getByRole("button", { name: "Confirm Schedule" }));
    expect(await screen.findByText("Readback Failed")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect((screen.getByRole("button", { name: "Publish Article" }) as HTMLButtonElement).disabled).toBe(true);
  });
  it("does not unlock a new or uncertain request using a historical cancellation", async () => {
    const key = await Publishing.articleDraftRecordKey(seed.id, seed.createdAt);
    scheduleRows.push({ scheduleUri: `at://${did}/at.skej.schedule/old`, status: "canceled", record: { recordType: "site.standard.document", publishRkey: key, status: "canceled", posts: [], shadowRecord: { $type: "site.standard.document", title: seed.title, site: publications[0]!.uri, publishedAt: "2099-01-01T12:00:00Z" } } });
    scheduleArticle.mockRejectedValue(new Error("Readback Failed"));
    await show();
    fireEvent.click(screen.getByRole("button", { name: "Schedule Article" }));
    await screen.findByRole("dialog");
    fireEvent.change(screen.getByLabelText("Publish Article At"), { target: { value: "2099-01-01T12:00" } });
    fireEvent.click(screen.getByRole("button", { name: "Confirm Schedule" }));
    await screen.findByText("Readback Failed");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(screen.queryByRole("button", { name: "Unlock Canceled Draft" })).toBeNull();
    expect((screen.getByRole("button", { name: "Publish Article" }) as HTMLButtonElement).disabled).toBe(true);
  });
  it("only unlocks the exact confirmed canceled schedule without an active replacement", async () => {
    seed.scheduleRecordKey = "stable-key";
    seed.scheduleUri = `at://${did}/at.skej.schedule/current`;
    const record = { recordType: "site.standard.document", publishRkey: seed.scheduleRecordKey, status: "canceled", posts: [], shadowRecord: { $type: "site.standard.document", title: seed.title, site: publications[0]!.uri, publishedAt: "2099-01-01T12:00:00Z" } };
    scheduleRows.push({ scheduleUri: seed.scheduleUri, status: "canceled", record });
    await show();
    fireEvent.click(screen.getByRole("button", { name: "Unlock Canceled Draft" }));
    await waitFor(() => expect((screen.getByRole("button", { name: "Publish Article" }) as HTMLButtonElement).disabled).toBe(false));
  });
  it("does not unlock a confirmed canceled schedule when a replacement is active", async () => {
    seed.scheduleRecordKey = "stable-key";
    seed.scheduleUri = `at://${did}/at.skej.schedule/current`;
    const record = { recordType: "site.standard.document", publishRkey: seed.scheduleRecordKey, status: "canceled", posts: [], shadowRecord: { $type: "site.standard.document", title: seed.title, site: publications[0]!.uri, publishedAt: "2099-01-01T12:00:00Z" } };
    scheduleRows.push({ scheduleUri: seed.scheduleUri, status: "canceled", record }, { scheduleUri: `at://${did}/at.skej.schedule/replacement`, status: "scheduled", record: { ...record, status: "scheduled" } });
    await show();
    expect(screen.queryByRole("button", { name: "Unlock Canceled Draft" })).toBeNull();
  });
  it("unlocks only a confirmed not-submitted scheduling failure", async () => {
    scheduleArticle.mockRejectedValue(new ArticleSchedulingNotSubmittedError(new Error("Connect Skej")));
    await show();
    fireEvent.click(screen.getByRole("button", { name: "Schedule Article" }));
    await screen.findByRole("dialog");
    fireEvent.change(screen.getByLabelText("Publish Article At"), { target: { value: "2099-01-01T12:00" } });
    fireEvent.click(screen.getByRole("button", { name: "Confirm Schedule" }));
    expect(await screen.findByText("Connect Skej")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect((screen.getByRole("button", { name: "Publish Article" }) as HTMLButtonElement).disabled).toBe(false);
  });
  it("rejects unsupported image scheduling without publishing or uploading", async () => {
    seed.markdown = "![private](article-asset://image)";
    const upload = spyOn(Publishing, "uploadArticleImage"); restores.push(() => upload.mockRestore());
    await show();
    fireEvent.click(screen.getByRole("button", { name: "Schedule Article" }));
    await screen.findByRole("dialog");
    fireEvent.change(screen.getByLabelText("Publish Article At"), { target: { value: "2099-01-01T12:00" } });
    fireEvent.click(screen.getByRole("button", { name: "Confirm Schedule" }));
    expect(await screen.findByText(/Scheduling Supports Text Articles/)).toBeTruthy();
    expect(scheduleArticle).not.toHaveBeenCalled();
    expect(upload).not.toHaveBeenCalled();
  });
  it("never schedules when persisting the snapshot fails", async () => {
    persist.mockRejectedValue(new Error("Storage Full"));
    await show();
    fireEvent.click(screen.getByRole("button", { name: "Schedule Article" }));
    await screen.findByRole("dialog");
    fireEvent.change(screen.getByLabelText("Publish Article At"), { target: { value: "2099-01-01T12:00" } });
    fireEvent.click(screen.getByRole("button", { name: "Confirm Schedule" }));
    expect(await screen.findByText("Storage Full")).toBeTruthy();
    expect(scheduleArticle).not.toHaveBeenCalled();
  });
  it("toggles metadata without losing editor or metadata edits", async () => {
    await show();
    const title = screen.getByLabelText("Article Title");
    fireEvent.change(title, { target: { value: "Still Writing" } });
    fireEvent.change(screen.getByLabelText("Article Excerpt"), { target: { value: "Retained Metadata" } });
    fireEvent.keyDown(screen.getByRole("separator", { name: "Resize Metadata" }), { key: "ArrowLeft", shiftKey: true });
    fireEvent.click(screen.getByRole("button", { name: "Hide Metadata" }));
    expect(screen.queryByRole("complementary", { name: "Article Library" })).toBeNull();
    expect(screen.queryByRole("complementary", { name: "Article Details" })).toBeNull();
    expect(screen.getByLabelText("Article Title")).toBe(title);
    fireEvent.click(screen.getByRole("button", { name: "Show Metadata" }));
    expect((screen.getByLabelText("Article Title") as HTMLInputElement).value).toBe("Still Writing");
    expect((screen.getByLabelText("Article Excerpt") as HTMLTextAreaElement).value).toBe("Retained Metadata");
    expect(screen.getByRole("separator", { name: "Resize Metadata" }).getAttribute("aria-valuenow")).toBe("344");
    const sidebarToggle = screen.getByRole("button", { name: "Toggle Sidebar" });
    expect(sidebarToggle.className).not.toContain("md:hidden");
    const expanded = sidebarToggle.getAttribute("aria-expanded");
    fireEvent.click(sidebarToggle);
    expect(sidebarToggle.getAttribute("aria-expanded")).not.toBe(expanded);
    fireEvent.click(sidebarToggle);
    expect(sidebarToggle.getAttribute("aria-expanded")).toBe(expanded);
  });
  it("resizes metadata with keyboard bounds", async () => {
    await show();
    const metadata = screen.getByRole("separator", { name: "Resize Metadata" });
    fireEvent.keyDown(metadata, { key: "ArrowLeft" });
    expect(metadata.getAttribute("aria-valuenow")).toBe("328");
    fireEvent.keyDown(metadata, { key: "End" });
    fireEvent.keyDown(metadata, { key: "ArrowLeft" });
    expect(metadata.getAttribute("aria-valuenow")).toBe("480");
    fireEvent.keyDown(metadata, { key: "Home" });
    fireEvent.keyDown(metadata, { key: "ArrowRight" });
    expect(metadata.getAttribute("aria-valuenow")).toBe("208");
  });
  it("renders the actual block editor and publishes only after explicit confirmation", async () => {
    const publish = publishingSpy();
    await show();
    expect(screen.getByText("A private paragraph.")).toBeTruthy();
    expect(publish).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Publish Article" }));
    expect(await screen.findByRole("dialog")).toBeTruthy();
    expect(publish).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(publish).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Publish Article" }));
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("button", { name: "Publish Now" }));
    await waitFor(() => expect(publish).toHaveBeenCalledTimes(1));
    expect(publish.mock.calls[0]![1]).toMatchObject({ title: seed.title, markdown: seed.markdown, publication: publications[0] });
  });
  it("blocks immediate publishing when another tab has scheduled the clean draft", async () => {
    const publish = publishingSpy();
    await show();
    // The editor still holds the original object while IndexedDB has the new lock.
    storedDraft = { ...storedDraft, scheduleRecordKey: "scheduled-in-another-tab" };
    fireEvent.click(screen.getByRole("button", { name: "Publish Article" }));
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("button", { name: "Publish Now" }));
    await waitFor(() => expect(screen.getAllByRole("alert").some(alert => alert.textContent?.includes("Review It Before Publishing"))).toBe(true));
    expect(publish).not.toHaveBeenCalled();
  });
  it("does not publish an old snapshot when a remote edit has the same timestamp", async () => {
    const publish = publishingSpy();
    await show();
    storedDraft = { ...storedDraft, title: "A Different Remote Title" };
    fireEvent.click(screen.getByRole("button", { name: "Publish Article" }));
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("button", { name: "Publish Now" }));
    await waitFor(() => expect(screen.getAllByRole("alert").some(alert => alert.textContent?.includes("Review It Before Publishing"))).toBe(true));
    expect(publish).not.toHaveBeenCalled();
  });
  it("requires an acknowledged private claim before making any public article write", async () => {
    const publish = publishingSpy();
    persist.mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error("Private Claim Sync Failed"));
    await show();
    fireEvent.click(screen.getByRole("button", { name: "Publish Article" }));
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("button", { name: "Publish Now" }));
    await waitFor(() => expect(screen.getAllByRole("alert").some(alert => alert.textContent?.includes("Private Claim Sync Failed"))).toBe(true));
    expect(publish).not.toHaveBeenCalled();
    expect(storedDraft.publishingRecordKey).toBeUndefined();
    expect(storedDraft.publicationClaimId).toBeUndefined();
  });
  it("stops when another device wins the private publication claim", async () => {
    const publish = publishingSpy();
    persist.mockImplementation(async () => {
      if (storedDraft.publishingRecordKey) storedDraft = { ...storedDraft, publicationClaimId: "00000000-0000-4000-8000-000000000001" };
    });
    await show();
    fireEvent.click(screen.getByRole("button", { name: "Publish Article" }));
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("button", { name: "Publish Now" }));
    await waitFor(() => expect(screen.getAllByRole("alert").some(alert => alert.textContent?.includes("Another Device Is Publishing"))).toBe(true));
    expect(publish).not.toHaveBeenCalled();
    expect(storedDraft.publicationClaimId).toBe("00000000-0000-4000-8000-000000000001");
  });
  it("releases its private claim when an image is missing before article submission", async () => {
    seed.assets = [{ id: "missing", name: "Cover.png", mimeType: "image/png", width: 10, height: 10, alt: "" }];
    seed.coverAssetId = "missing";
    const blob = spyOn(Storage, "getArticleDraftAsset").mockResolvedValue(undefined); restores.push(() => blob.mockRestore());
    const publish = publishingSpy();
    await show();
    fireEvent.click(screen.getByRole("button", { name: "Publish Article" }));
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("button", { name: "Publish Now" }));
    await waitFor(() => expect(screen.getAllByRole("alert").some(alert => alert.textContent?.includes("is missing"))).toBe(true));
    expect(publish).not.toHaveBeenCalled();
    await waitFor(() => expect(persist.mock.calls.length).toBeGreaterThanOrEqual(3));
    await waitFor(() => expect(storedDraft.publishingRecordKey).toBeUndefined());
    expect(storedDraft.publicationClaimId).toBeUndefined();
  });
  it("reconciles a lost publication response with an owned matching record without publishing again", async () => {
    seed.publishingRecordKey = "stable-key"; seed.publicationClaimId = "own-claim";
    const uri = `at://${did}/site.standard.document/stable-key`;
    publishedRows.push({ uri, cid: "existing", record: { $type: "site.standard.document", site: seed.publicationUri, title: seed.title } });
    const publish = publishingSpy();
    await show();
    fireEvent.click(screen.getByRole("button", { name: "Check Publication" }));
    await waitFor(() => expect(storedDraft.publishedUri).toBe(uri));
    expect(storedDraft.publishingRecordKey).toBeUndefined();
    expect(storedDraft.publicationClaimId).toBe("own-claim");
    expect(persist).toHaveBeenCalledWith({ requireRemote: true });
    expect(publish).not.toHaveBeenCalled();
  });
  it("keeps an uncertain publication protected when no matching publication is found", async () => {
    seed.publishingRecordKey = "stable-key"; seed.publicationClaimId = "own-claim";
    publishedRows.push({ uri: `at://${did}/site.standard.document/stable-key`, cid: "different", record: { $type: "site.standard.document", site: publications[1]!.uri, title: seed.title } });
    const publish = publishingSpy();
    await show();
    fireEvent.click(screen.getByRole("button", { name: "Check Publication" }));
    await waitFor(() => expect(screen.getAllByRole("alert").some(alert => alert.textContent?.includes("Still Unconfirmed"))).toBe(true));
    expect(storedDraft.publishingRecordKey).toBe("stable-key");
    expect(storedDraft.publishedUri).toBeUndefined();
    expect(publish).not.toHaveBeenCalled();
  });
  it("recovers an acknowledged schedule after a lost response so confirmed cancellation can unlock it", async () => {
    seed.scheduleRecordKey = "stable-key"; seed.publicationClaimId = "own-claim";
    const scheduleUri = `at://${did}/at.skej.schedule/current`;
    scheduleRows.push({ did, scheduleUri, status: "canceled", record: { recordType: "site.standard.document", publishRkey: seed.scheduleRecordKey, status: "canceled", posts: [], shadowRecord: { $type: "site.standard.document", title: seed.title, site: seed.publicationUri, publishedAt: "2099-01-01T12:00:00Z" } } });
    await show();
    fireEvent.click(screen.getByRole("button", { name: "Check Schedule" }));
    await waitFor(() => expect(storedDraft.scheduleUri).toBe(scheduleUri));
    fireEvent.click(await screen.findByRole("button", { name: "Unlock Canceled Draft" }));
    await waitFor(() => expect(storedDraft.scheduleRecordKey).toBeUndefined());
    expect(storedDraft.publicationClaimId).toBeUndefined();
    expect(scheduleArticle).not.toHaveBeenCalled();
  });
  it("retains the private claim when an explicit publication result is uncertain", async () => {
    const publish = publishingSpy(); publish.mockRejectedValue(new Error("Publication Response Was Lost"));
    await show();
    fireEvent.click(screen.getByRole("button", { name: "Publish Article" }));
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("button", { name: "Publish Now" }));
    await waitFor(() => expect(publish).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(storedDraft.publishingRecordKey).toBeTruthy());
    expect(storedDraft.publicationClaimId).toBeTruthy();
    expect((screen.getByLabelText("Article Title") as HTMLInputElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Publish Article", hidden: true }) as HTMLButtonElement).disabled).toBe(true);
  });
  it("requires title, content, and a selected owned publication before confirmation", async () => {
    const publish = publishingSpy(); seed.title = ""; seed.publicationUri = "";
    await show();
    expect((screen.getByRole("button", { name: "Publish Article" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Article Title"), { target: { value: "Ready" } });
    expect((screen.getByRole("button", { name: "Publish Article" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Article Publication"), { target: { value: publications[1]!.uri } });
    await waitFor(() => expect((screen.getByRole("button", { name: "Publish Article" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Markdown" }));
    fireEvent.change(screen.getByLabelText("Article Markdown"), { target: { value: " " } });
    expect((screen.getByRole("button", { name: "Publish Article" }) as HTMLButtonElement).disabled).toBe(true);
    expect(publish).not.toHaveBeenCalled();
  });
  it("passes the chosen publication and relative path through confirmation", async () => {
    const publish = publishingSpy();
    await show();
    fireEvent.change(screen.getByLabelText("Article Publication"), { target: { value: publications[1]!.uri } });
    fireEvent.change(screen.getByLabelText("Article Path"), { target: { value: "/chosen-path" } });
    fireEvent.click(screen.getByRole("button", { name: "Publish Article" }));
    expect(await screen.findByText(/to Second Publication on your public PDS/)).toBeTruthy();
    expect(publish).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Publish Now" }));
    await waitFor(() => expect(publish).toHaveBeenCalledTimes(1));
    expect(publish.mock.calls[0]![1]).toMatchObject({ publication: publications[1], path: "/chosen-path" });
  });
  it("shows storage errors and preserves edits when Save Draft fails", async () => {
    storageError = "Browser Storage Is Full";
    persist.mockRejectedValue(new Error("Browser Storage Is Full"));
    const publish = publishingSpy();
    await show();
    expect(screen.getByRole("alert").textContent).toContain("Browser Storage Is Full");
    fireEvent.click(screen.getByRole("button", { name: "Save Draft" }));
    await waitFor(() => expect(persist).toHaveBeenCalledTimes(1));
    expect((screen.getByLabelText("Article Title") as HTMLInputElement).value).toBe(seed.title);
    fireEvent.click(screen.getByRole("button", { name: "Publish Article" }));
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("button", { name: "Publish Now" }));
    await waitFor(() => expect(persist).toHaveBeenCalledTimes(2));
    expect(publish).not.toHaveBeenCalled();
    expect(screen.getAllByRole("alert").some(alert => alert.textContent?.includes("Browser Storage Is Full"))).toBe(true);
  });
  it("disables publication and article editing for an already published local draft", async () => {
    seed.publishedUri = `at://${did}/site.standard.document/existing`;
    const publish = publishingSpy();
    await show();
    for (const label of ["Article Title", "Article Publication", "Article Path", "Article Excerpt", "Article Tags"]) expect((screen.getByLabelText(label) as HTMLInputElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Publish Article" }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText(/Published article editing will be available/)).toBeTruthy();
    expect(publish).not.toHaveBeenCalled();
  });
});

let previousSchedulingFlag: string | undefined;
beforeEach(() => { previousSchedulingFlag = process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING; process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING = "true"; });
afterEach(() => { if (previousSchedulingFlag === undefined) delete process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING; else process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING = previousSchedulingFlag; });
