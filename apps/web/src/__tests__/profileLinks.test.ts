import { afterEach, describe, expect, it, spyOn } from "bun:test";
import { loadProfileLinks } from "@/lib/profileLinks";
import * as Network from "@/lib/atprotoNetwork";

const did = "did:plc:abcdefghijklmnopqrstuvwx", other = "did:plc:bcdefghijklmnopqrstuvwxy";
const cid = "bafyreigdyrzt5sfp7udm7hu76uh7y26nf3pt5y6okqac52uhicb2o54y5u";
const iconCID = "bafkreigh2akiscaildcv6ubnt36d2bk6bj3fx3xpq6xfj2vgtz3yqjv2zy";
const pds = "https://profile-pds.test";
const ref = (rkey: string, owner = did) => ({ uri: `at://${owner}/app.bsky.actor.link/${rkey}`, cid });
const doc = (owner = did, endpoint = pds) => ({ id: owner, service: [{ id: "#atproto_pds", type: "AtprotoPersonalDataServer", serviceEndpoint: endpoint }] });
const profile = (links: unknown = [ref("first")], owner = did) => ({ uri: `at://${owner}/app.bsky.actor.profile/self`, cid, value: { $type: "app.bsky.actor.profile", links } });
const link = (rkey: string, patch: Record<string, unknown> = {}, owner = did) => ({ ...ref(rkey, owner), value: { $type: "app.bsky.actor.link", url: `https://${rkey}.example/path`, createdAt: "2026-11-09T00:00:00.000Z", title: `Link ${rkey}`, ...patch } });
const restores: (() => void)[] = [];
const oldAllowlist = process.env.NEXT_PUBLIC_LOCAL_ATPROTO_PROFILE_PDS_URLS;
afterEach(() => {
  restores.splice(0).reverse().forEach(restore => restore());
  if (oldAllowlist === undefined) delete process.env.NEXT_PUBLIC_LOCAL_ATPROTO_PROFILE_PDS_URLS;
  else process.env.NEXT_PUBLIC_LOCAL_ATPROTO_PROFILE_PDS_URLS = oldAllowlist;
});
function network(isolated = false) {
  const spy = spyOn(Network, "getAtprotoNetwork").mockReturnValue(Network.resolveAtprotoNetwork(isolated ? "local" : "production", isolated ? { plcDirectory: "https://plc.local.test", publicAppView: "https://appview.local.test" } : {}));
  restores.push(() => spy.mockRestore());
}
function serve(handler: (url: URL, init?: RequestInit) => unknown | Response | Promise<unknown | Response>) {
  const requests: { url: URL; init?: RequestInit }[] = [];
  const spy = spyOn(globalThis, "fetch").mockImplementation(Object.assign(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input)); requests.push({ url, init });
    const result = await handler(url, init);
    return result instanceof Response ? result : Response.json(result);
  }, { preconnect: globalThis.fetch.preconnect }));
  restores.push(() => spy.mockRestore());
  return requests;
}
function basic(url: URL) {
  if (url.host === "plc.directory") return doc();
  if (url.searchParams.get("collection") === "app.bsky.actor.profile") return profile();
  return link(url.searchParams.get("rkey")!);
}

describe("profile actor link cards", () => {
  it("loads only pinned strong references in display order with version-specific requests and no credentials", async () => {
    network();
    const requests = serve(url => {
      if (url.host === "plc.directory") return doc();
      if (url.searchParams.get("collection") === "app.bsky.actor.profile") return profile([ref("second"), ref("first")]);
      return link(url.searchParams.get("rkey")!);
    });
    expect(await loadProfileLinks(did)).toEqual([
      { uri: ref("second").uri, url: "https://second.example/path", title: "Link second", domain: "second.example", iconUrl: undefined },
      { uri: ref("first").uri, url: "https://first.example/path", title: "Link first", domain: "first.example", iconUrl: undefined },
    ]);
    expect(requests).toHaveLength(4);
    for (const { url, init } of requests) {
      expect(url.pathname).not.toContain("listRecords");
      expect(init?.credentials).toBe("omit");
      expect(init?.redirect).toBe("error");
      expect(new Headers(init?.headers).has("authorization")).toBe(false);
      if (url.searchParams.get("collection") === "app.bsky.actor.link") expect(url.searchParams.get("cid")).toBe(cid);
    }
  });
  it("uses only the owner's PDS for valid public icon blobs", async () => {
    network();
    serve(url => url.searchParams.get("collection") === "app.bsky.actor.link" ? link("first", { icon: { $type: "blob", ref: { $link: iconCID }, mimeType: "image/webp", size: 100_000 } }) : basic(url));
    const cards = await loadProfileLinks(did);
    expect(cards[0].iconUrl).toBe(`${pds}/xrpc/com.atproto.sync.getBlob?${new URLSearchParams({ did, cid: iconCID })}`);
  });
  it("skips foreign owners, collections, malformed keys/CIDs, duplicates, and references beyond the first ten", async () => {
    network();
    const refs = [ref("foreign", other), { ...ref("wrong"), uri: `at://${did}/app.bsky.feed.post/wrong` }, ref("../bad"), { ...ref("badcid"), cid: "bad" }, ref("first"), ref("first"), ...["a", "b", "c", "d", "ignored"].map(key => ref(key))];
    const requests = serve(url => url.searchParams.get("collection") === "app.bsky.actor.profile" ? profile(refs) : basic(url));
    expect((await loadProfileLinks(did)).map(card => card.title)).toEqual(["Link first", "Link a", "Link b", "Link c", "Link d"]);
    expect(requests.filter(request => request.url.searchParams.get("collection") === "app.bsky.actor.link").map(request => request.url.searchParams.get("rkey"))).toEqual(["first", "a", "b", "c", "d"]);
  });
  it("bounds all link reads to ten even when the stored profile exceeds schema limits", async () => {
    network();
    const requests = serve(url => url.searchParams.get("collection") === "app.bsky.actor.profile" ? profile(Array.from({ length: 30 }, (_, index) => ref(`link${index}`))) : basic(url));
    expect(await loadProfileLinks(did)).toHaveLength(10);
    expect(requests).toHaveLength(12);
  });
  it("skips missing, malformed, insecure and mismatched individual cards without losing valid siblings", async () => {
    network();
    const patches = new Map<string, unknown>([
      ["missing", Response.json({ error: "RecordNotFound" }, { status: 400 })],
      ["cid", { ...link("cid"), cid: iconCID }],
      ["uri", { ...link("uri"), uri: ref("other").uri }],
      ["type", link("type", { $type: "app.bsky.feed.post" })],
      ["http", link("http", { url: "http://example.com" })],
      ["credentials", link("credentials", { url: "https://user:secret@example.com" })],
      ["title", link("title", { title: "x".repeat(41) })],
      ["date", link("date", { createdAt: "tomorrow" })],
      ["icon", link("icon", { icon: { $type: "blob", ref: { $link: iconCID }, mimeType: "image/svg+xml", size: 10 } })],
      ["valid", link("valid", { title: undefined })],
    ]);
    serve(url => url.searchParams.get("collection") === "app.bsky.actor.profile" ? profile([...patches.keys()].map(key => ref(key))) : url.searchParams.get("collection") === "app.bsky.actor.link" ? patches.get(url.searchParams.get("rkey")!) : basic(url));
    expect((await loadProfileLinks(did)).map(card => card.domain)).toEqual(["valid.example"]);
  });
  it("does not infer cards from website fields or unpinned records", async () => {
    network();
    const requests = serve(url => url.host === "plc.directory" ? doc() : { ...profile(), value: { $type: "app.bsky.actor.profile", website: "https://website.example" } });
    expect(await loadProfileLinks(did)).toEqual([]);
    expect(requests).toHaveLength(2);
  });
  it("enforces title byte limits and icon size/CID constraints independently of grapheme counts", async () => {
    network();
    const rows = new Map([
      ["bytes", link("bytes", { title: "👩‍👩‍👧‍👦".repeat(40) })],
      ["size", link("size", { icon: { $type: "blob", ref: { $link: iconCID }, mimeType: "image/png", size: 100_001 } })],
      ["cid", link("cid", { icon: { $type: "blob", ref: { $link: "bad" }, mimeType: "image/png", size: 10 } })],
      ["valid", link("valid", { title: "é".repeat(40) })],
    ]);
    serve(url => url.searchParams.get("collection") === "app.bsky.actor.profile" ? profile([...rows.keys()].map(key => ref(key))) : url.searchParams.get("collection") === "app.bsky.actor.link" ? rows.get(url.searchParams.get("rkey")!) : basic(url));
    expect((await loadProfileLinks(did)).map(card => card.title)).toEqual(["é".repeat(40)]);
  });
  it("fails on identity, profile record and discovery errors rather than treating unavailable data as empty", async () => {
    network();
    serve(url => url.host === "plc.directory" ? doc(other) : basic(url));
    await expect(loadProfileLinks(did)).rejects.toThrow("does not match");
  });
  it.each([
    { ...profile(), uri: `at://${other}/app.bsky.actor.profile/self` },
    { ...profile(), cid: "bad" },
    { ...profile(), value: { $type: "wrong", links: [] } },
    { ...profile(), value: { $type: "app.bsky.actor.profile", links: "bad" } },
  ])("rejects invalid canonical profile records", async invalid => {
    network(); serve(url => url.host === "plc.directory" ? doc() : invalid);
    await expect(loadProfileLinks(did)).rejects.toThrow();
  });
  it("returns no links only for a missing profile record, while exposing request failures", async () => {
    network();
    serve(url => url.host === "plc.directory" ? doc() : Response.json({ error: "RecordNotFound" }, { status: 400 }));
    expect(await loadProfileLinks(did)).toEqual([]);
  });
  it("propagates discovery authentication/network errors", async () => {
    network();
    serve(() => Response.json({ error: "AuthRequired" }, { status: 401 }));
    await expect(loadProfileLinks(did)).rejects.toThrow("401");
  });
  it("supports DID-web paths while rejecting credentials, traversal and unsafe PDS URLs", async () => {
    network();
    const owner = "did:web:example.com:people:alice";
    const requests = serve(url => url.host === "example.com" ? doc(owner) : url.searchParams.get("collection") === "app.bsky.actor.profile" ? profile([], owner) : undefined);
    expect(await loadProfileLinks(owner)).toEqual([]);
    expect(requests[0].url.href).toBe("https://example.com/people/alice/did.json");
    await expect(loadProfileLinks("did:web:user%40example.com")).rejects.toThrow();
    await expect(loadProfileLinks("did:web:example.com:..:alice")).rejects.toThrow();
  });
  it.each(["http://pds.example", "https://user:secret@pds.example", "https://pds.example/?secret=yes", "https://pds.example/#fragment"])("rejects unsafe PDS endpoint %s", async endpoint => {
    network(); serve(() => doc(did, endpoint));
    await expect(loadProfileLinks(did)).rejects.toThrow("PDS endpoint");
  });
  it("keeps isolated local reads on configured PLC and explicitly allowed PDS origins", async () => {
    network(true); process.env.NEXT_PUBLIC_LOCAL_ATPROTO_PROFILE_PDS_URLS = pds;
    const requests = serve(url => url.host === "plc.local.test" ? doc() : url.searchParams.get("collection") === "app.bsky.actor.profile" ? profile([]) : undefined);
    expect(await loadProfileLinks(did)).toEqual([]);
    expect(requests[0].url.origin).toBe("https://plc.local.test");
    await expect(loadProfileLinks("did:web:example.com")).rejects.toThrow("isolated local");
  });
  it("refuses unlisted isolated PDS endpoints before any PDS request", async () => {
    network(true); delete process.env.NEXT_PUBLIC_LOCAL_ATPROTO_PROFILE_PDS_URLS;
    const requests = serve(() => doc());
    await expect(loadProfileLinks(did)).rejects.toThrow("not allowed");
    expect(requests).toHaveLength(1);
  });
  it("passes cancellation to every read and never converts an aborted card read to empty success", async () => {
    network(); const controller = new AbortController();
    const requests = serve(url => { if (url.searchParams.get("collection") === "app.bsky.actor.link") controller.abort(); return basic(url); });
    await expect(loadProfileLinks(did, controller.signal)).rejects.toThrow();
    expect(requests.every(request => request.init?.signal === controller.signal)).toBe(true);
    const count = requests.length;
    await expect(loadProfileLinks(did, controller.signal)).rejects.toThrow();
    expect(requests).toHaveLength(count);
  });
});
