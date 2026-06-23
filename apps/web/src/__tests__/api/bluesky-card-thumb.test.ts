import { afterEach, beforeEach, describe, expect, it, mock } from "bun:test";

import { GET } from "@/app/api/bluesky-card-thumb/route";

const ORIGINAL_FETCH = globalThis.fetch;

function requestFor(target: string): Request {
  const params = new URLSearchParams({ url: target });
  return new Request(`https://thesocialwire.app/api/bluesky-card-thumb?${params}`);
}

describe("GET /api/bluesky-card-thumb", () => {
  beforeEach(() => {
    globalThis.fetch = ORIGINAL_FETCH;
  });

  afterEach(() => {
    globalThis.fetch = ORIGINAL_FETCH;
    mock.restore();
  });

  it("proxies public image bytes", async () => {
    const fetchMock = mock(async (url: URL) => {
      expect(url.href).toBe("https://cdn.example/og.png");
      return new Response(new Uint8Array([1, 2, 3]), {
        headers: {
          "content-length": "3",
          "content-type": "image/png",
        },
      });
    });
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    const res = await GET(requestFor("https://cdn.example/og.png"));

    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("image/png");
    expect(new Uint8Array(await res.arrayBuffer())).toEqual(
      new Uint8Array([1, 2, 3])
    );
  });

  it("rejects private or non-HTTPS targets", async () => {
    const res = await GET(requestFor("http://localhost/og.png"));
    expect(res.status).toBe(400);
  });

  it("rejects non-image responses", async () => {
    globalThis.fetch = mock(async () =>
      new Response("nope", {
        headers: { "content-type": "text/html" },
      })
    ) as unknown as typeof fetch;

    const res = await GET(requestFor("https://cdn.example/page"));
    expect(res.status).toBe(415);
  });

  it("rejects images over the Bluesky external-card size limit", async () => {
    globalThis.fetch = mock(async () =>
      new Response(new Uint8Array([1]), {
        headers: {
          "content-length": "1000001",
          "content-type": "image/jpeg",
        },
      })
    ) as unknown as typeof fetch;

    const res = await GET(requestFor("https://cdn.example/large.jpg"));
    expect(res.status).toBe(413);
  });
});
