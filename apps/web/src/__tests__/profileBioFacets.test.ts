import { afterEach, describe, expect, it, spyOn } from "bun:test";
import { AppBskyRichtextFacet, RichText } from "@atproto/api";
import { detectProfileBioFacets, resolveProfileBioFacets } from "@/lib/profileBioFacets";
import * as Network from "@/lib/atprotoNetwork";

const did = "did:plc:abcdefghijklmnopqrstuvwx";
const restores: (() => void)[] = [];
afterEach(() => restores.splice(0).reverse().forEach(restore => restore()));
function transport(response: (url: URL, init?: RequestInit) => Response | Promise<Response> = () => Response.json({ did })) {
  const requests: { url: URL; init?: RequestInit }[] = [];
  const fetcher = spyOn(globalThis, "fetch").mockImplementation(Object.assign(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input)); requests.push({ url, init }); return response(url, init);
  }, { preconnect: globalThis.fetch.preconnect }));
  restores.push(() => fetcher.mockRestore());
  return requests;
}
function segments(text: string, facets: AppBskyRichtextFacet.Main[]) {
  return [...new RichText({ text, facets }).segments()].filter(segment => !!segment.facet).map(segment => ({ text: segment.text, features: segment.facet!.features }));
}
function mentionDIDs(facets: AppBskyRichtextFacet.Main[]) { return facets.flatMap(facet => facet.features.filter(AppBskyRichtextFacet.isMention).map(feature => feature.did)); }

describe("profile bio facets", () => {
  it("detects links, hashtags and unresolved mentions without changing UTF-8 offsets or profile text", () => {
    const text = "👩‍💻 café (@alice.test), https://example.com/path. #ATProto!";
    const requests = transport();
    const facets = detectProfileBioFacets(text);
    expect(requests).toHaveLength(0);
    expect(segments(text, facets).map(segment => segment.text)).toEqual(["@alice.test", "https://example.com/path", "#ATProto"]);
    expect(facets[0].index.byteStart).toBe(new TextEncoder().encode("👩‍💻 café (").length);
    expect(mentionDIDs(facets)).toEqual(["alice.test"]);
    expect(new RichText({ text, facets }).text).toBe(text);
  });
  it("resolves a unique normalized handle once while preserving every mention's byte range", async () => {
    const text = "🙂 @Alice.test and @alice.test #news https://example.com";
    const requests = transport();
    const facets = await resolveProfileBioFacets(text);
    expect(requests).toHaveLength(1);
    expect(requests[0].url.searchParams.get("handle")).toBe("alice.test");
    expect(mentionDIDs(facets)).toEqual([did, did]);
    expect(segments(text, facets).map(segment => segment.text)).toEqual(["@Alice.test", "@alice.test", "#news", "https://example.com"]);
    expect(facets.map(facet => facet.index)).toEqual(detectProfileBioFacets(text).map(facet => facet.index));
  });
  it("uses only the configured public AppView with credentials omitted and cancellation forwarded", async () => {
    const network = spyOn(Network, "getAtprotoNetwork").mockReturnValue(Network.resolveAtprotoNetwork("local", { publicAppView: "https://appview.local.test", plcDirectory: "https://plc.local.test" }));
    restores.push(() => network.mockRestore());
    const requests = transport(); const controller = new AbortController();
    await resolveProfileBioFacets("@alice.test", controller.signal);
    expect(requests[0].url.origin).toBe("https://appview.local.test");
    expect(requests[0].url.pathname).toBe("/xrpc/com.atproto.identity.resolveHandle");
    expect(requests[0].init?.credentials).toBe("omit");
    expect(requests[0].init?.redirect).toBe("error");
    expect(requests[0].init?.signal).toBe(controller.signal);
    expect(new Headers(requests[0].init?.headers).has("authorization")).toBe(false);
  });
  it("leaves failed mentions plain without losing valid mentions, links or hashtags", async () => {
    const text = "@alice.test @missing.test https://example.com #news";
    transport(url => url.searchParams.get("handle") === "missing.test" ? Response.json({ error: "HandleNotFound" }, { status: 400 }) : Response.json({ did }));
    const facets = await resolveProfileBioFacets(text);
    expect(mentionDIDs(facets)).toEqual([did]);
    expect(segments(text, facets).map(segment => segment.text)).toEqual(["@alice.test", "https://example.com", "#news"]);
    expect(new RichText({ text, facets }).text).toBe(text);
  });
  it.each([{}, { did: "not-a-did" }, { did: "did:plc:invalid?returnTo=evil" }, { did: 1 }])("never creates a mention target from invalid resolver data", async data => {
    transport(() => Response.json(data));
    expect(await resolveProfileBioFacets("@alice.test")).toEqual([]);
  });
  it("does not link unsafe URLs or syntactically invalid handles", () => {
    const text = "http://example.com https://user:secret@example.com javascript:alert(1) @-bad.test";
    expect(detectProfileBioFacets(text)).toEqual([]);
  });
  it("preserves plain-text Markdown syntax and detects only actual URLs", () => {
    const text = "**Name** [Site](https://example.com) `@alice.test`";
    const facets = detectProfileBioFacets(text);
    expect(new RichText({ text, facets }).text).toBe(text);
    expect(segments(text, facets).map(segment => segment.text)).toEqual(["https://example.com"]);
  });
  it("does no identity requests when the bio has no mentions", async () => {
    const requests = transport();
    expect(await resolveProfileBioFacets("https://example.com #news")).toEqual(detectProfileBioFacets("https://example.com #news"));
    expect(requests).toHaveLength(0);
    expect(detectProfileBioFacets("")).toEqual([]);
  });
  it("treats network and malformed JSON failures as plain mentions", async () => {
    transport(async url => {
      if (url.searchParams.get("handle") === "alice.test") throw new Error("Offline");
      return new Response("{bad JSON", { headers: { "Content-Type": "application/json" } });
    });
    expect(await resolveProfileBioFacets("@alice.test @bob.test #news")).toEqual(detectProfileBioFacets("#news").map(facet => ({ ...facet, index: { byteStart: 22, byteEnd: 27 } })));
  });
  it("propagates cancellation before and during resolution without caching stale results", async () => {
    const controller = new AbortController();
    const requests = transport(async () => { controller.abort(); return Response.json({ did }); });
    await expect(resolveProfileBioFacets("@alice.test", controller.signal)).rejects.toThrow();
    expect(requests).toHaveLength(1);
    await expect(resolveProfileBioFacets("@alice.test", controller.signal)).rejects.toThrow();
    expect(requests).toHaveLength(1);
    expect(mentionDIDs(await resolveProfileBioFacets("@alice.test"))).toEqual([did]);
    expect(requests).toHaveLength(2);
  });
});
