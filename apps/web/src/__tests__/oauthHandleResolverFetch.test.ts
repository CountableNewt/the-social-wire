import { describe, expect, it } from "bun:test";
import { BrowserOAuthClient, XrpcHandleResolver } from "@atproto/oauth-client-browser";
import { atprotoLoopbackClientMetadata } from "@atproto/oauth-types";
import { IDBFactory } from "fake-indexeddb";
import { createOAuthHandleResolverFetch } from "@/lib/oauthHandleResolverFetch";

const primary = "https://public.api.bsky.app/xrpc/com.atproto.identity.resolveHandle?handle=testing.stygiantech.dev";
const fallback = primary.replace("public.api.bsky.app", "bsky.social");
const did = "did:plc:zu7vdjfbiijes5rjcaaqtzke";

function fixture(results: Array<Response | Error | ((init?: RequestInit) => Promise<Response>)>) {
  const calls: Array<{ url: string; init?: RequestInit }> = [];
  const base = Object.assign(async (input: RequestInfo | URL, init?: RequestInit) => {
    calls.push({ url: input instanceof Request ? input.url : String(input), init });
    const result = results.shift();
    if (result instanceof Error) throw result;
    if (typeof result === "function") return result(init);
    if (!result) throw new Error("Unexpected request");
    return result;
  }, { preconnect: fetch.preconnect }) as typeof fetch;
  return { calls, fetch: createOAuthHandleResolverFetch(base) };
}

describe("OAuth handle resolution fallback", () => {
  it("uses the public resolver response without another request", async () => {
    const response = Response.json({ did });
    const f = fixture([response]);
    expect(await f.fetch(primary)).toBe(response);
    expect(f.calls.map(call => call.url)).toEqual([primary]);
  });

  for (const failure of [new TypeError("Load failed"), 429, 500, 503]) {
    it(`retries ${String(failure)} once through the alternate identity endpoint`, async () => {
      const controller = new AbortController();
      const init: RequestInit = { signal: controller.signal, cache: "no-cache", redirect: "error" };
      const response = Response.json({ did });
      const f = fixture([typeof failure === "number" ? new Response(null, { status: failure }) : failure, response]);
      expect(await f.fetch(primary, init)).toBe(response);
      expect(f.calls.map(call => call.url)).toEqual([primary, fallback]);
      expect(f.calls[1].init).toBe(init);
    });
  }

  for (const status of [200, 400, 401, 403, 404]) {
    it(`does not retry HTTP ${status} even with an invalid or missing DID`, async () => {
      const response = Response.json({ error: "InvalidRequest", message: "Unable to resolve handle" }, { status });
      const f = fixture([response]);
      expect(await f.fetch(primary)).toBe(response);
      expect(f.calls).toHaveLength(1);
    });
  }

  it("does not retry caller cancellation", async () => {
    const controller = new AbortController();
    const f = fixture([async () => { controller.abort(); throw new TypeError("Load failed"); }]);
    await expect(f.fetch(primary, { signal: controller.signal })).rejects.toThrow("Load failed");
    expect(f.calls).toHaveLength(1);
  });

  it("preserves Request options and its cancellation signal on fallback", async () => {
    const controller = new AbortController();
    const request = new Request(primary, { signal: controller.signal, redirect: "error", cache: "no-cache" });
    const response = Response.json({ did });
    const inputs: Request[] = [];
    const base = Object.assign(async (input: RequestInfo | URL) => {
      inputs.push(input as Request);
      return inputs.length === 1 ? new Response(null, { status: 503 }) : response;
    }, { preconnect: fetch.preconnect }) as typeof fetch;
    expect(await createOAuthHandleResolverFetch(base)(request)).toBe(response);
    expect(inputs[1].url).toBe(fallback);
    expect(inputs[1].redirect).toBe("error");
    expect(inputs[1].cache).toBe("no-cache");
    controller.abort();
    expect(inputs[1].signal.aborted).toBe(true);
  });

  it("does not retry programming errors", async () => {
    const f = fixture([new Error("Unexpected failure")]);
    await expect(f.fetch(primary)).rejects.toThrow("Unexpected failure");
    expect(f.calls).toHaveLength(1);
  });

  for (const url of [primary.replace("public.api.bsky.app", "pds.atmosbox.test"), "https://plc.directory/" + did, "https://bsky.social/oauth/token"]) {
    it(`keeps ${url} on its original host`, async () => {
      const f = fixture([new TypeError("Load failed")]);
      await expect(f.fetch(url)).rejects.toThrow("Load failed");
      expect(f.calls.map(call => call.url)).toEqual([url]);
    });
  }

  const privateRequests: RequestInit[] = [{ method: "POST" }, { headers: { Authorization: "DPoP fixture" } }, { headers: { DPoP: "fixture" } }];
  for (const init of privateRequests) {
    it(`does not forward authenticated requests or mutations: ${JSON.stringify(init)}`, async () => {
      const f = fixture([new Response(null, { status: 503 })]);
      expect((await f.fetch(primary, init)).status).toBe(503);
      expect(f.calls).toHaveLength(1);
    });
  }

  it("propagates fallback failure without looping", async () => {
    const f = fixture([new Response(null, { status: 503 }), new TypeError("Alternate unavailable")]);
    await expect(f.fetch(primary)).rejects.toThrow("Alternate unavailable");
    expect(f.calls).toHaveLength(2);
  });

  it("leaves response validation with the SDK", async () => {
    const f = fixture([new TypeError("Load failed"), Response.json({ did: "invalid" })]);
    const resolver = new XrpcHandleResolver("https://public.api.bsky.app", { fetch: f.fetch });
    await expect(resolver.resolve("testing.stygiantech.dev")).rejects.toThrow("Invalid DID");
    expect(f.calls).toHaveLength(2);
  });

  it("returns the fallback DID through the SDK resolver", async () => {
    const f = fixture([new TypeError("Load failed"), Response.json({ did })]);
    const resolver = new XrpcHandleResolver("https://public.api.bsky.app", { fetch: f.fetch });
    expect(await resolver.resolve("testing.stygiantech.dev")).toBe(did);
  });

  for (const alias of ["testing.stygiantech.dev", "different.example"]) {
    it(`checks the fallback DID against its PLC alias: ${alias}`, async () => {
      const prior = Object.getOwnPropertyDescriptor(globalThis, "indexedDB");
      Object.defineProperty(globalThis, "indexedDB", { configurable: true, value: new IDBFactory() });
      const f = fixture([
        new TypeError("Load failed"), Response.json({ did }),
        Response.json({ id: did, alsoKnownAs: [`at://${alias}`] }),
      ]);
      const client = new BrowserOAuthClient({
        clientMetadata: atprotoLoopbackClientMetadata("http://localhost"),
        handleResolver: "https://public.api.bsky.app",
        fetch: f.fetch,
      });
      try {
        const resolution = client.identityResolver.resolve("testing.stygiantech.dev");
        if (alias === "testing.stygiantech.dev") expect((await resolution).did).toBe(did);
        else await expect(resolution).rejects.toThrow("does not include the handle");
        expect(f.calls.map(call => call.url)).toEqual([primary, fallback, `https://plc.directory/${encodeURIComponent(did)}`]);
      } finally {
        await client[Symbol.asyncDispose]();
        if (prior) Object.defineProperty(globalThis, "indexedDB", prior);
        else Reflect.deleteProperty(globalThis, "indexedDB");
      }
    });
  }
});
