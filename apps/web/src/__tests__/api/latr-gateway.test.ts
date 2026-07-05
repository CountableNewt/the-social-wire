import { afterEach, describe, expect, it, mock } from "bun:test";
import { NextRequest } from "next/server";

import { GET } from "@/app/api/latr-gateway/[...path]/route";

const ORIG_FETCH = globalThis.fetch;
const ORIG_ENV = {
  LATR_GATEWAY_CLIENT_CREDENTIAL: process.env.LATR_GATEWAY_CLIENT_CREDENTIAL,
  LATR_GATEWAY_CLIENT_ID: process.env.LATR_GATEWAY_CLIENT_ID,
  LATR_GATEWAY_API_KEY: process.env.LATR_GATEWAY_API_KEY,
  LATR_GATEWAY_URL: process.env.LATR_GATEWAY_URL,
  VERCEL_URL: process.env.VERCEL_URL,
  NEXT_PUBLIC_SITE_URL: process.env.NEXT_PUBLIC_SITE_URL,
  NEXT_PUBLIC_APP_ENV: process.env.NEXT_PUBLIC_APP_ENV,
  APP_ENV: process.env.APP_ENV,
};

function restoreEnv(): void {
  for (const [key, value] of Object.entries(ORIG_ENV)) {
    if (value === undefined) delete process.env[key];
    else process.env[key] = value;
  }
}

afterEach(() => {
  globalThis.fetch = ORIG_FETCH;
  restoreEnv();
});

describe("GET /api/latr-gateway/[...path]", () => {
  it("forwards user auth headers to the hosted upstream and keeps credentials server-only", async () => {
    process.env.LATR_GATEWAY_CLIENT_CREDENTIAL = "the-social-wire-web=official-secret";
    delete process.env.LATR_GATEWAY_CLIENT_ID;
    delete process.env.LATR_GATEWAY_API_KEY;
    delete process.env.LATR_GATEWAY_URL;
    process.env.VERCEL_URL = "testing.thesocialwire.app";
    process.env.NEXT_PUBLIC_APP_ENV = "test";

    let upstreamUrl: string | URL | Request | undefined;
    let upstreamHeaders = new Headers();
    const fetchMock = mock(async (url: string | URL | Request, init?: RequestInit) => {
      upstreamUrl = url;
      upstreamHeaders = new Headers(init?.headers);
      return new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: {
          "Content-Type": "application/json",
          "DPoP-Nonce": "gateway-nonce",
          "X-Latr-Official-Client": "must-not-leak",
        },
      });
    });
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    const req = new NextRequest(
      "https://testing.thesocialwire.app/api/latr-gateway/v1/latr/saves?limit=25",
      {
        headers: {
          Authorization: "DPoP user-token",
          DPoP: "same-origin-proof",
          "X-ATProto-Upstream-DPoP": "pds-proof",
          Accept: "application/json",
          "Content-Type": "application/json",
        },
      }
    );

    const res = await GET(req, {
      params: Promise.resolve({ path: ["v1", "latr", "saves"] }),
    });

    expect(res.status).toBe(200);
    expect(String(upstreamUrl)).toBe(
      "https://api.testing.latr.link/v1/latr/saves?limit=25"
    );
    expect(upstreamHeaders.get("Authorization")).toBe("DPoP user-token");
    expect(upstreamHeaders.get("DPoP")).toBe("same-origin-proof");
    expect(upstreamHeaders.get("X-ATProto-Upstream-DPoP")).toBe("pds-proof");
    expect(upstreamHeaders.get("Accept")).toBe("application/json");
    expect(upstreamHeaders.get("Content-Type")).toBe("application/json");
    expect(upstreamHeaders.get("X-Latr-Official-Client")).toBe("official-secret");
    expect(res.headers.get("DPoP-Nonce")).toBe("gateway-nonce");
    expect(res.headers.get("X-Latr-Official-Client")).toBeNull();
    expect(res.headers.get("X-Latr-Client-Id")).toBeNull();
    expect(res.headers.get("X-Latr-API-Key")).toBeNull();
  });
});
