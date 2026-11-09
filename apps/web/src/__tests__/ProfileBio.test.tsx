import { afterEach, beforeEach, describe, expect, it, spyOn } from "bun:test";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AppBskyRichtextFacet, RichText } from "@atproto/api";
import * as Bio from "@/lib/profileBioFacets";
import { ProfileBio } from "@/components/Account/ProfileBio";

let cache: QueryClient;
let resolve: ReturnType<typeof spyOn<typeof Bio, "resolveProfileBioFacets">>;
function facets(text: string, mentionDid?: string) {
  const rich = new RichText({ text });
  rich.detectFacetsWithoutResolution();
  return (rich.facets ?? []).map(facet => ({ ...facet, features: facet.features.filter(feature => !AppBskyRichtextFacet.isMention(feature) || !!mentionDid).map(feature => AppBskyRichtextFacet.isMention(feature) ? { ...feature, did: mentionDid! } : feature) })).filter(facet => facet.features.length);
}
function deferred() {
  let finish!: (value: AppBskyRichtextFacet.Main[]) => void;
  const promise = new Promise<AppBskyRichtextFacet.Main[]>(resolve => { finish = resolve; });
  return { promise, finish };
}
function show(did: string, text: string) {
  return render(<QueryClientProvider client={cache}><ProfileBio did={did} text={text} /></QueryClientProvider>);
}
beforeEach(() => {
  cache = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  resolve = spyOn(Bio, "resolveProfileBioFacets").mockImplementation(async text => facets(text));
});
afterEach(() => { cleanup(); cache.clear(); resolve.mockRestore(); });

describe("profile biography facets", () => {
  it("renders detected tags and safe links immediately while mentions resolve internally", async () => {
    const pending = deferred(); resolve.mockReturnValue(pending.promise);
    show("did:plc:viewer", "😀 Hello @alice.test #Bluesky https://example.com/path.");
    expect(screen.getByRole("link", { name: "#Bluesky" }).getAttribute("href")).toBe("https://bsky.app/search?q=%23Bluesky");
    const link = screen.getByRole("link", { name: "https://example.com/path" });
    expect(link.getAttribute("href")).toBe("https://example.com/path");
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.getAttribute("rel")).toBe("noopener noreferrer");
    expect(screen.queryByRole("link", { name: "@alice.test" })).toBeNull();
    await act(async () => pending.finish(facets("😀 Hello @alice.test #Bluesky https://example.com/path.", "did:plc:alice")));
    const mention = await screen.findByRole("link", { name: "@alice.test" });
    expect(mention.getAttribute("href")).toContain("/social/profile?actor=did%3Aplc%3Aalice");
  });

  it("preserves literal Markdown, HTML, newlines, emoji and unsafe destinations as text", async () => {
    const text = "😀 **literal**\n<img src=x> javascript:alert(1) http://example.com https://user:secret@example.com #Nature";
    const view = show("did:plc:viewer", text);
    expect(resolve).not.toHaveBeenCalled();
    expect(view.container.textContent).toBe(text);
    expect(view.container.querySelector("strong, img, script")).toBeNull();
    expect(screen.getAllByRole("link").length).toBe(1);
    expect(screen.getByRole("link", { name: "#Nature" })).toBeTruthy();
    expect(view.container.querySelector(".whitespace-pre-wrap")).toBeTruthy();
  });

  it("keeps a failed mention readable while independent links remain available", async () => {
    resolve.mockRejectedValue(new Error("public identity unavailable"));
    const view = show("did:plc:viewer", "Ask @missing.test #Help https://example.com");
    await waitFor(() => expect(cache.getQueryCache().getAll().some(query => query.state.status === "error")).toBe(true));
    expect(screen.queryByRole("link", { name: "@missing.test" })).toBeNull();
    expect(view.container.textContent).toContain("@missing.test");
    expect(screen.getByRole("link", { name: "#Help" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "https://example.com" })).toBeTruthy();
  });

  it.each(["text", "did"])("isolates late mention results after a %s change", async change => {
    const old = deferred();
    const fresh = deferred();
    resolve.mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise);
    const originalText = "Old @alice.test";
    const nextText = change === "text" ? "New @bob.test" : originalText;
    const view = show("did:plc:first", originalText);
    await waitFor(() => expect(resolve).toHaveBeenCalledTimes(1));
    view.rerender(<QueryClientProvider client={cache}><ProfileBio did={change === "did" ? "did:plc:second" : "did:plc:first"} text={nextText} /></QueryClientProvider>);
    await waitFor(() => expect(resolve).toHaveBeenCalledTimes(2));
    expect(resolve.mock.calls[0]?.[1]?.aborted).toBe(true);
    await act(async () => fresh.finish(facets(nextText, "did:plc:fresh")));
    const label = change === "text" ? "@bob.test" : "@alice.test";
    expect((await screen.findByRole("link", { name: label })).getAttribute("href")).toContain("did%3Aplc%3Afresh");
    await act(async () => old.finish(facets(originalText, "did:plc:stale")));
    expect(screen.getByRole("link", { name: label }).getAttribute("href")).toContain("did%3Aplc%3Afresh");
    expect(view.container.textContent).toBe(nextText);
    expect(view.container.querySelector('a[href*="stale"]')).toBeNull();
  });
});
