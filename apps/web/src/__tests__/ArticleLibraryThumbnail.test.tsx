import { afterEach, beforeEach, expect, it, spyOn } from "bun:test";
import { act, cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { jsonStringToLex } from "@atproto/lexicon";
import * as Storage from "@/lib/articles/articleDraftStorage";
import * as Atproto from "@/lib/atprotoClient";
import * as CachedImages from "@/hooks/useCachedImageUrl";
import type { ArticleDraft } from "@/lib/articles/articleDraftTypes";
import type { PublishedArticle } from "@/lib/articles/articlePublishingTypes";
import { ArticleLibraryThumbnail } from "@/components/Articles/ArticleLibraryThumbnail";

const did = "did:plc:author";
const restores: (() => void)[] = [];
let client: QueryClient;
const draft = (coverAssetId?: string): ArticleDraft => ({ id: "draft-one", title: "Draft", markdown: "", excerpt: "", path: "", tags: [], publicationUri: "", createdAt: "2026-11-01T00:00:00Z", updatedAt: "2026-11-01T00:00:00Z", coverAssetId });
const article: PublishedArticle = { uri: `at://${did}/site.standard.document/one`, cid: "article", record: {} };
beforeEach(() => { client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } }); });
afterEach(() => { cleanup(); client.clear(); restores.splice(0).reverse().forEach(restore => restore()); });
function wrapper({ children }: { children: ReactNode }) { return <QueryClientProvider client={client}>{children}</QueryClientProvider>; }

it("loads private covers from the viewer's draft store and revokes replaced/unmounted object URLs", async () => {
  const blob = new Blob(["cover"], { type: "image/png" });
  const asset = spyOn(Storage, "getArticleDraftAsset").mockResolvedValue(blob); restores.push(() => asset.mockRestore());
  const create = spyOn(URL, "createObjectURL").mockReturnValueOnce("blob:first-cover").mockReturnValueOnce("blob:second-cover"); restores.push(() => create.mockRestore());
  const revoke = spyOn(URL, "revokeObjectURL").mockImplementation(() => {}); restores.push(() => revoke.mockRestore());
  const view = render(<ArticleLibraryThumbnail did={did} draft={draft("cover-one")} />, { wrapper });
  await waitFor(() => expect(view.container.querySelector("img")?.getAttribute("src")).toBe("blob:first-cover"));
  expect(asset).toHaveBeenCalledWith(did, "draft-one", "cover-one");
  view.rerender(<ArticleLibraryThumbnail did={did} draft={draft("cover-two")} />);
  await waitFor(() => expect(view.container.querySelector("img")?.getAttribute("src")).toBe("blob:second-cover"));
  expect(revoke).toHaveBeenCalledWith("blob:first-cover");
  view.unmount();
  expect(revoke).toHaveBeenCalledWith("blob:second-cover");
});

it("ignores a stale cover read after another draft cover becomes active", async () => {
  const oldBlob = new Blob(["old"]), newBlob = new Blob(["new"]);
  let finishOld!: (blob: Blob) => void;
  const asset = spyOn(Storage, "getArticleDraftAsset").mockImplementation(async (_did, _draft, cover) => cover === "old" ? new Promise<Blob>(resolve => { finishOld = resolve; }) : newBlob); restores.push(() => asset.mockRestore());
  const create = spyOn(URL, "createObjectURL").mockReturnValue("blob:new-cover"); restores.push(() => create.mockRestore());
  const revoke = spyOn(URL, "revokeObjectURL").mockImplementation(() => {}); restores.push(() => revoke.mockRestore());
  const view = render(<ArticleLibraryThumbnail did={did} draft={draft("old")} />, { wrapper });
  view.rerender(<ArticleLibraryThumbnail did={did} draft={draft("new")} />);
  await waitFor(() => expect(view.container.querySelector("img")?.getAttribute("src")).toBe("blob:new-cover"));
  await act(async () => { finishOld(oldBlob); });
  expect(create).toHaveBeenCalledTimes(1);
  expect(create).toHaveBeenCalledWith(newBlob);
  expect(view.container.querySelector("img")?.getAttribute("src")).toBe("blob:new-cover");
});

it("falls back to the cached secondary remote image after the primary fails", async () => {
  const thumbnail = spyOn(Atproto, "resolveEntryThumbnailUrls").mockResolvedValue({ thumbnailUrl: "https://images.test/primary.png", thumbnailFallbackUrl: "https://images.test/fallback.png" }); restores.push(() => thumbnail.mockRestore());
  const cache = spyOn(CachedImages, "useCachedImageUrl").mockImplementation(src => ({ objectUrl: src || undefined, failed: false })); restores.push(() => cache.mockRestore());
  const view = render(<ArticleLibraryThumbnail did={did} article={article} />, { wrapper });
  await waitFor(() => expect(view.container.querySelector("img")?.getAttribute("src")).toBe("https://images.test/primary.png"));
  expect(thumbnail).toHaveBeenCalledWith(article.uri, article.record);
  fireEvent.error(view.container.querySelector("img")!);
  await waitFor(() => expect(view.container.querySelector("img")?.getAttribute("src")).toBe("https://images.test/fallback.png"));
  fireEvent.error(view.container.querySelector("img")!);
  expect(view.container.querySelector("img")).toBeNull();
  expect(view.container.querySelector("svg")).toBeTruthy();
});

it("normalizes a real decoded XRPC cover blob before resolving its thumbnail", async () => {
  const cid = "bafkreie3zvlvg43mywvbvld32d7sk6xkgvjphhhgtjgwqhzfmvf6xnuw2m";
  const rawRecord = { $type: "site.standard.document", coverImage: { $type: "blob", ref: { $link: cid }, mimeType: "image/png", size: 3 } };
  const decoded = jsonStringToLex(JSON.stringify(rawRecord)) as PublishedArticle["record"];
  const cover = decoded.coverImage as { ref: { $link?: string; toString(): string } };
  // The XRPC agent returns a BlobRef/CID rather than the JSON $link shape that
  // the shared image resolver reads. Keep this fixture decoded to exercise it.
  expect(cover.ref.$link).toBeUndefined();
  expect(cover.ref.toString()).toBe(cid);
  const thumbnail = spyOn(Atproto, "resolveEntryThumbnailUrls").mockImplementation(async (_uri, record) => {
    const value = record as typeof rawRecord;
    return value.coverImage.ref.$link === cid ? { thumbnailUrl: "https://pds.test/cover.png" } : {};
  }); restores.push(() => thumbnail.mockRestore());
  const cache = spyOn(CachedImages, "useCachedImageUrl").mockImplementation(src => ({ objectUrl: src || undefined, failed: false })); restores.push(() => cache.mockRestore());
  const view = render(<ArticleLibraryThumbnail did={did} article={{ ...article, record: decoded }} />, { wrapper });
  await waitFor(() => expect(view.container.querySelector("img")?.getAttribute("src")).toBe("https://pds.test/cover.png"));
  expect(thumbnail).toHaveBeenCalledWith(article.uri, rawRecord);
  expect(cover.ref.$link).toBeUndefined();
});

it("keeps a placeholder when draft or remote artwork is missing", async () => {
  const asset = spyOn(Storage, "getArticleDraftAsset").mockResolvedValue(undefined); restores.push(() => asset.mockRestore());
  const thumbnail = spyOn(Atproto, "resolveEntryThumbnailUrls").mockResolvedValue({}); restores.push(() => thumbnail.mockRestore());
  const view = render(<ArticleLibraryThumbnail did={did} draft={draft("missing")} />, { wrapper });
  await waitFor(() => expect(asset).toHaveBeenCalled());
  expect(view.container.querySelector("img")).toBeNull();
  await act(async () => { view.rerender(<ArticleLibraryThumbnail did={did} article={article} />); });
  await waitFor(() => expect(client.getQueryState(["articles", did, "thumbnail", article.uri, article.cid])?.status).toBe("success"));
  expect(view.container.querySelector("img")).toBeNull();
  expect(view.container.querySelector("svg")).toBeTruthy();
});
