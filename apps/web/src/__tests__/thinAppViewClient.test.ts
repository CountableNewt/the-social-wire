import { describe, expect, it, mock, beforeEach, afterEach } from "bun:test";

const ORIG_ENV = { ...process.env };
const ORIG_FETCH = globalThis.fetch;

describe("thinAppViewClient", () => {
  beforeEach(() => {
    process.env.NEXT_PUBLIC_USE_THIN_APPVIEW = "true";
    process.env.NEXT_PUBLIC_SOCIALWIRE_API_URL = "https://api.example.test";
    globalThis.fetch = ORIG_FETCH;
  });

  afterEach(() => {
    process.env = { ...ORIG_ENV };
    globalThis.fetch = ORIG_FETCH;
    mock.restore();
  });

  it("isThinAppViewEnabled is true by default", async () => {
    delete process.env.NEXT_PUBLIC_USE_THIN_APPVIEW;
    const { isThinAppViewEnabled } = await import("@/lib/thinAppViewClient");
    expect(isThinAppViewEnabled()).toBe(true);
    process.env.NEXT_PUBLIC_USE_THIN_APPVIEW = "false";
    expect(isThinAppViewEnabled()).toBe(false);
  });

  it("preserves typed publisher failures but keeps generic enrollment errors as failures", async () => {
    const { enrollAuthorsInAppView } = await import("@/lib/thinAppViewClient");
    const { PublicationSourceRefreshError } = await import("@/lib/publicationSourceRefreshError");
    for (const partial of [true, false]) {
      const session = gatewayOAuthSession(mock(async () => Response.json({
        error: "Service Unavailable", message: "Enrollment unavailable",
        ...(partial ? { articlesUpdated: true, unavailablePublicationIds: ["rss:world"], sourceFailures: [{ category: "rss_backoff", kind: "other", host: "publisher.test", count: 1 }] } : {}),
      }, { status: 503 })));
      const error = await enrollAuthorsInAppView(session, [], ["https://publisher.test/rss"]).catch(error => error);
      expect(error).toBeInstanceOf(Error);
      expect(error instanceof PublicationSourceRefreshError).toBe(partial);
      if (error instanceof PublicationSourceRefreshError) expect(error.unavailablePublicationIds).toEqual(["rss:world"]);
    }
  });

  it("listEntriesFromAppView requires appViewScope and sends scope params", async () => {
    const fetchHandler = mock(async (url: string) => {
      expect(url).toContain("publicationScopeAtUris=");
      expect(url).toContain("publicationSiteUrls=");
      expect(url).toContain("filter=unread");
      return new Response(
        JSON.stringify({
          entries: [
            {
              entryId: "at://did:plc:alice/site.standard.entry/rkey1",
              title: "Indexed",
              publishedAt: "2024-01-01T00:00:00.000Z",
            },
          ],
          cursor: "next-cursor",
        }),
        { status: 200, headers: { "Content-Type": "application/json" } }
      );
    });
    const oauthSession = gatewayOAuthSession(fetchHandler);

    const { listEntriesFromAppView } = await import("@/lib/thinAppViewClient");
    const page = await listEntriesFromAppView({
      publicationKey: "at://did:plc:alice/site.standard.publication/main",
      appViewScope: {
        authorDid: "did:plc:alice",
        publicationAtUri: "at://did:plc:alice/site.standard.publication/main",
        publicationScopeAtUris: [
          "at://did:plc:alice/com.standard.publication/main",
        ],
        publicationSiteUrls: ["https://example.offprint.app"],
      },
      filter: "unread",
      oauthSession,
    });

    expect(page.entries).toHaveLength(1);
    expect(page.entries[0]?.title).toBe("Indexed");
    expect(page.cursor).toBe("next-cursor");
    expect(fetchHandler).toHaveBeenCalledTimes(1);
  });

  it("listEntriesFromAppView sends maxEntries instead of cursor", async () => {
    const fetchHandler = mock(async (url: string) => {
      expect(url).toContain("maxEntries=120");
      expect(url).not.toContain("cursor=");
      return new Response(
        JSON.stringify({ entries: [], cursor: "more" }),
        { status: 200, headers: { "Content-Type": "application/json" } }
      );
    });
    const oauthSession = gatewayOAuthSession(fetchHandler);

    const { listEntriesFromAppView } = await import("@/lib/thinAppViewClient");
    await listEntriesFromAppView({
      publicationKey: "at://did:plc:alice/site.standard.publication/main",
      appViewScope: {
        authorDid: "did:plc:alice",
        publicationAtUri: "at://did:plc:alice/site.standard.publication/main",
        publicationScopeAtUris: [],
        publicationSiteUrls: [],
      },
      maxEntries: 120,
      oauthSession,
    });
    expect(fetchHandler).toHaveBeenCalledTimes(1);
  });

  it("normalizes numeric Swift dates returned by the aggregate feed", async () => {
    const fetchHandler = mock(async (url: string) => {
      expect(url).toContain("/xrpc/app.thesocialwire.appview.getFeed?");
      return new Response(
        JSON.stringify({
          entries: [
            {
              entryId: "at://did:plc:alice/site.standard.document/entry1",
              title: "Indexed",
              publishedAt: 725_760_000,
              isRead: false,
            },
          ],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } }
      );
    });
    const oauthSession = gatewayOAuthSession(fetchHandler);

    const { listAggregateFeedFromAppView } = await import(
      "@/lib/thinAppViewClient"
    );
    const page = await listAggregateFeedFromAppView({
      feed: { kind: "subscribed" },
      oauthSession,
    });

    expect(page.entries[0]?.publishedAt).toBe("2024-01-01T00:00:00.000Z");
  });

  it("getEntryFromAppView fetches entry detail from gateway", async () => {
    const fetchHandler = mock(async (url: string) => {
      expect(url).toContain("/xrpc/app.thesocialwire.appview.getEntry?");
      expect(url).toContain("entryId=");
      return new Response(
        JSON.stringify({
          entryId: "at://did:plc:alice/site.standard.document/entry1",
          title: "Detail",
          summary: "Detail summary",
          publishedAt: "2024-01-01T00:00:00.000Z",
          thumbnailUrl: "https://cdn.example/detail.jpg",
          thumbnailFallbackUrl: "https://cdn.example/detail-fallback.jpg",
          contentHtml: "<p>Hi</p>",
        }),
        { status: 200, headers: { "Content-Type": "application/json" } }
      );
    });
    const oauthSession = gatewayOAuthSession(fetchHandler);

    const { getEntryFromAppView } = await import("@/lib/thinAppViewClient");
    const entry = await getEntryFromAppView(
      oauthSession,
      "at://did:plc:alice/site.standard.document/entry1"
    );
    expect(entry?.title).toBe("Detail");
    expect(entry?.summary).toBe("Detail summary");
    expect(entry?.thumbnailUrl).toBe("https://cdn.example/detail.jpg");
    expect(entry?.thumbnailFallbackUrl).toBe(
      "https://cdn.example/detail-fallback.jpg"
    );
    expect(fetchHandler).toHaveBeenCalledTimes(1);
  });

  it.each(["publication", "aggregate", "detail"] as const)("preserves recognized content labels through the %s mapping without changing legacy responses", async kind => {
    const api = await import("@/lib/thinAppViewClient");
    for (const contentLabels of [undefined, ["sexual", "ai-generated", "unknown", "sexual"]]) {
      const row = { entryId: "at://did:plc:publisher/site.standard.document/one", title: "Visible Story", contentHtml: "<p>Visible</p>", publishedAt: "2026-11-09", isRead: false, ...(contentLabels ? { contentLabels } : {}) };
      const oauthSession = gatewayOAuthSession(mock(async () => Response.json(kind === "detail" ? row : { entries: [row] })));
      const entry = kind === "detail" ? await api.getEntryFromAppView(oauthSession, row.entryId) : (kind === "aggregate" ? await api.listAggregateFeedFromAppView({ feed: { kind: "subscribed" }, oauthSession }) : await api.listEntriesFromAppView({ publicationKey: "publisher", appViewScope: { authorDid: "did:plc:publisher", publicationAtUri: null, publicationScopeAtUris: [], publicationSiteUrls: [] }, oauthSession })).entries[0];
      expect(entry?.title).toBe("Visible Story");
      if (contentLabels) expect(entry?.contentLabels).toEqual(["ai-generated", "sexual"]);
      else expect(entry).not.toHaveProperty("contentLabels");
    }
  });

  it("writeThroughReadMark posts subjectUri and readAt", async () => {
    const fetchHandler = mock(async (url: string, init?: RequestInit) => {
      expect(url).toBe(
        "https://api.example.test/xrpc/app.thesocialwire.appview.putReadMark"
      );
      expect(init?.method).toBe("POST");
      const body = JSON.parse(String(init?.body));
      expect(body.subjectUri).toBe("at://did:plc:alice/site.standard.entry/rkey1");
      expect(body.readAt).toBe("2024-06-01T12:00:00.000Z");
      return new Response(null, { status: 200 });
    });
    const oauthSession = gatewayOAuthSession(fetchHandler);

    const { writeThroughReadMark } = await import("@/lib/thinAppViewClient");
    await writeThroughReadMark(
      oauthSession,
      "at://did:plc:alice/site.standard.entry/rkey1",
      "2024-06-01T12:00:00.000Z"
    );
    expect(fetchHandler).toHaveBeenCalledTimes(1);
  });

  it("fetchAppViewUnreadCounts returns count snapshot metadata", async () => {
    const fetchHandler = mock(async (url: string) => {
      expect(url).toContain(
        "/xrpc/app.thesocialwire.appview.getUnreadCounts?"
      );
      const request = new URL(url);
      expect(request.searchParams.getAll("publicationIds")).toEqual([
        "did:plc:alice",
        "at://did:plc:alice/site.standard.publication/main",
      ]);
      return new Response(
        JSON.stringify({
          counts: { "did:plc:alice": 2 },
          generation: 42,
          accuracy: "exact",
          countedAt: "2026-01-01T00:00:00.000Z",
        }),
        { status: 200, headers: { "Content-Type": "application/json" } }
      );
    });
    const oauthSession = gatewayOAuthSession(fetchHandler);

    const { fetchAppViewUnreadCounts } = await import("@/lib/thinAppViewClient");
    const snapshot = await fetchAppViewUnreadCounts(
      oauthSession,
      [
        "did:plc:alice",
        "at://did:plc:alice/site.standard.publication/main",
      ]
    );
    expect(snapshot.counts["did:plc:alice"]).toBe(2);
    expect(snapshot.generation).toBe(42);
    expect(snapshot.accuracy).toBe("exact");
  });
});

function gatewayOAuthSession(
  gatewayHandler: (url: string, init?: RequestInit) => Promise<Response>
) {
  globalThis.fetch = (async (url: string, init?: RequestInit) => {
    const request = new URL(url);
    if (request.pathname === "/xrpc/app.thesocialwire.appview.listEntries" &&
      request.searchParams.get("authorDid") === "did:plc:viewer" && request.searchParams.get("limit") === "1") {
      expect(request.searchParams.size).toBe(2);
      return Response.json({}, { headers: { "X-ATProto-Session-Attestation-Receipt": "reader-receipt" } });
    }
    return gatewayHandler(url, init);
  }) as unknown as typeof fetch;
  const nonces = new Map<string, string>();
  let nonceIndex = 0;
  return {
    did: "did:plc:viewer",
    getTokenSet: async () => ({
      access_token: "access-token",
      token_type: "DPoP",
    }),
    getTokenInfo: async () => ({ aud: "https://pds.example" }),
    fetchHandler: async () => {
      const nonce = `pds-nonce-${++nonceIndex}`;
      nonces.set("https://pds.example", nonce);
      return new Response(JSON.stringify({ records: [] }), {
        headers: { "DPoP-Nonce": nonce },
      });
    },
    server: {
      dpopKey: {
        bareJwk: { kty: "EC", crv: "P-256", x: "x", y: "y" },
        algorithms: ["ES256"],
        createJwt: async () => "dpop-proof",
      },
      dpopNonces: {
        get: async (origin: string) => nonces.get(origin),
        set: async (origin: string, nonce: string) => {
          nonces.set(origin, nonce);
        },
      },
      serverMetadata: { dpop_signing_alg_values_supported: ["ES256"] },
    },
  } as never;
}
