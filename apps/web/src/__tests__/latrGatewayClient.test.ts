import { afterEach, describe, expect, it, mock } from "bun:test";

import { resetLatrGatewayAuthRejectedForTests } from "@/lib/latrGatewayCredentials";
import {
  LATR_CLIENT_ID_HEADER,
  LATR_OFFICIAL_CLIENT_HEADER,
  LATR_UPSTREAM_DPOP_HEADER,
  latrGatewayFetch,
} from "@/lib/latrGatewayClient";
import { LATR_GATEWAY_PROXY_PREFIX } from "@/lib/latrGatewayProxyPath";

const ORIG_FETCH = globalThis.fetch;
const ORIG_LOCATION_DESCRIPTOR = Object.getOwnPropertyDescriptor(
  globalThis,
  "location"
);

afterEach(() => {
  resetLatrGatewayAuthRejectedForTests();
  globalThis.fetch = ORIG_FETCH;
  if (ORIG_LOCATION_DESCRIPTOR) {
    Object.defineProperty(globalThis, "location", ORIG_LOCATION_DESCRIPTOR);
  } else {
    delete (globalThis as { location?: Location }).location;
  }
});

describe("latrGatewayFetch", () => {
  it("calls the same-origin proxy and signs DPoP for that proxy URL", async () => {
    Object.defineProperty(globalThis, "location", {
      configurable: true,
      value: new URL("https://testing.thesocialwire.app/saved"),
    });
    let dpopClaims: Record<string, string | number> | undefined;

    const fetchMock = mock(async (url: string, init?: RequestInit) => {
      expect(url).toBe(`${LATR_GATEWAY_PROXY_PREFIX}/v1/latr/og-preview?url=https://example.com`);
      const headers = new Headers(init?.headers);
      expect(headers.get("Authorization")).toBe("DPoP access-token");
      expect(headers.get("DPoP")).toBe("gateway-dpop-proof");
      expect(headers.get(LATR_CLIENT_ID_HEADER)).toBeNull();
      expect(headers.get(LATR_OFFICIAL_CLIENT_HEADER)).toBeNull();
      return new Response(JSON.stringify({ ok: true }), { status: 200 });
    });
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    const oauthSession = {
      getTokenSet: async () => ({
        access_token: "access-token",
        token_type: "DPoP",
      }),
      server: {
        dpopKey: {
          bareJwk: { kty: "EC", crv: "P-256", x: "x", y: "y" },
          algorithms: ["ES256"],
          createJwt: async (_header: unknown, claims: Record<string, string | number>) => {
            dpopClaims = claims;
            return "gateway-dpop-proof";
          },
        },
        dpopNonces: {
          get: async () => undefined,
          set: async () => {},
        },
        serverMetadata: { dpop_signing_alg_values_supported: ["ES256"] },
      },
    } as never;

    await latrGatewayFetch(oauthSession, "/v1/latr/og-preview?url=https://example.com", {
      method: "GET",
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(dpopClaims?.htm).toBe("GET");
    expect(dpopClaims?.htu).toBe(
      "https://testing.thesocialwire.app/api/latr-gateway/v1/latr/og-preview"
    );
    expect(dpopClaims?.ath).toBeTruthy();
  });

  it("retries once when the proxy returns a DPoP nonce challenge", async () => {
    Object.defineProperty(globalThis, "location", {
      configurable: true,
      value: new URL("https://testing.thesocialwire.app/saved"),
    });
    let proxyCalls = 0;
    let nonceCounter = 0;
    const gatewayNonces = new Map<string, string>();
    const dpopClaims: Array<Record<string, string | number>> = [];

    const fetchMock = mock(async (...args: Parameters<typeof fetch>) => {
      const [url] = args;
      if (String(url).includes("/v1/latr/saves")) {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return new Response(JSON.stringify({ error: "Unauthorized" }), {
            status: 401,
            headers: { "DPoP-Nonce": "fresh-nonce" },
          });
        }
        return new Response(JSON.stringify({ ok: true }), { status: 200 });
      }

      nonceCounter += 1;
      return new Response(JSON.stringify({ error: "Use DPoP nonce" }), {
        status: 400,
        headers: { "DPoP-Nonce": `pds-nonce-${nonceCounter}` },
      });
    });
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    const oauthSession = {
      getTokenSet: async () => ({
        access_token: "access-token",
        token_type: "DPoP",
      }),
      getTokenInfo: async () => ({ aud: "https://jellybaby.us-east.host.bsky.network" }),
      fetchHandler: fetchMock as unknown as typeof fetch,
      server: {
        dpopNonces: {
          get: async (origin: string) => gatewayNonces.get(origin),
          set: async (origin: string, nonce: string) => {
            gatewayNonces.set(origin, nonce);
          },
        },
        dpopKey: {
          bareJwk: { kty: "EC", crv: "P-256", x: "x", y: "y" },
          algorithms: ["ES256"],
          createJwt: async (_header: unknown, claims: Record<string, string | number>) => {
            dpopClaims.push(claims);
            return "gateway-dpop-proof";
          },
        },
        serverMetadata: { dpop_signing_alg_values_supported: ["ES256"] },
      },
    } as never;

    const res = await latrGatewayFetch(oauthSession, "/v1/latr/saves", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ kind: "subject", subjectUri: "at://did/entry" }),
    });

    expect(res.status).toBe(200);
    expect(proxyCalls).toBe(2);
    // POST /v1/latr/saves mints 4 upstream proofs (2× createRecord, 2× putRecord) per attempt.
    expect(nonceCounter).toBe(8);

    const saveCall = fetchMock.mock.calls.find(([url]) =>
      String(url).includes("/v1/latr/saves")
    );
    expect(saveCall).toBeDefined();
    const saveHeaders = new Headers(saveCall?.[1]?.headers);
    expect(saveHeaders.get(LATR_UPSTREAM_DPOP_HEADER)).toBeTruthy();
    expect(gatewayNonces.get("https://testing.thesocialwire.app")).toBe(
      "fresh-nonce"
    );
    const gatewayBoundClaims = dpopClaims.filter((claims) =>
      String(claims.htu).includes("/api/latr-gateway/")
    );
    expect(gatewayBoundClaims[0]?.htu).toBe(
      "https://testing.thesocialwire.app/api/latr-gateway/v1/latr/saves"
    );
    expect(gatewayBoundClaims[1]?.nonce).toBe("fresh-nonce");
  });
});
