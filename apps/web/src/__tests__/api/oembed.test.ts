import { describe, expect, it } from "bun:test";
import { NextRequest } from "next/server";

import { GET } from "@/app/api/oembed/route";

describe("GET /api/oembed", () => {
  it("returns 400 when url param missing", async () => {
    const req = new NextRequest("http://localhost/api/oembed");
    const res = await GET(req);
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.error).toBe("missing url");
  });

  it("returns 400 for non-https target", async () => {
    const req = new NextRequest(
      "http://localhost/api/oembed?url=" +
        encodeURIComponent("http://insecure.example/article")
    );
    const res = await GET(req);
    expect(res.status).toBe(400);
  });
});

it("creates fresh fetch deadlines for repeated warm-instance requests", async () => {
  const originalFetch = globalThis.fetch;
  const signals: AbortSignal[] = [];
  globalThis.fetch = (async (_url: string | URL | Request, init?: RequestInit) => {
    signals.push(init!.signal!);
    return new Response("{}", { headers: { "content-type": "application/json" } });
  }) as unknown as typeof fetch;
  try {
    for (let count = 0; count < 2; count++) {
      const response = await GET(new NextRequest("https://thesocialwire.app/api/oembed?url=https%3A%2F%2Fpublisher.example%2Farticle"));
      expect(response.status).toBe(200);
    }
    expect(signals.length).toBe(4);
    expect(new Set(signals).size).toBe(signals.length);
    expect(signals.every(signal => !signal.aborted)).toBe(true);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
