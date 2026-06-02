import { describe, expect, it } from "bun:test";
import {
  pdsXrpcMethodForGatewayRequest,
  pdsXrpcMethodForSocialWireGatewayRequest,
  refreshPdsDpopNonceForGateway,
} from "@/lib/latrGatewayUpstreamDpop";

describe("pdsXrpcMethodForGatewayRequest", () => {
  it("maps POST /v1/latr/saves to createRecord", () => {
    expect(pdsXrpcMethodForGatewayRequest("POST", "/v1/latr/saves")).toEqual({
      xrpcMethod: "com.atproto.repo.createRecord",
      httpMethod: "POST",
    });
  });

  it("maps PATCH state routes to putRecord", () => {
    expect(
      pdsXrpcMethodForGatewayRequest(
        "PATCH",
        "/v1/latr/saves/ABC123/state"
      )
    ).toEqual({
      xrpcMethod: "com.atproto.repo.putRecord",
      httpMethod: "POST",
    });
  });

  it("maps DELETE item routes to deleteRecord", () => {
    expect(
      pdsXrpcMethodForGatewayRequest("DELETE", "/v1/latr/saves/ABC123")
    ).toEqual({
      xrpcMethod: "com.atproto.repo.deleteRecord",
      httpMethod: "POST",
    });
  });

  it("maps GET /v1/latr/saves to listRecords", () => {
    expect(pdsXrpcMethodForGatewayRequest("GET", "/v1/latr/saves")).toEqual({
      xrpcMethod: "com.atproto.repo.listRecords",
      httpMethod: "GET",
    });
  });
});

describe("pdsXrpcMethodForSocialWireGatewayRequest", () => {
  it("maps POST mark-all-read to putRecord", () => {
    expect(
      pdsXrpcMethodForSocialWireGatewayRequest(
        "POST",
        "/v1/appview/mark-all-read"
      )
    ).toEqual({
      xrpcMethod: "com.atproto.repo.putRecord",
      httpMethod: "POST",
    });
  });
});

describe("refreshPdsDpopNonceForGateway", () => {
  it("reuses cached nonce when listRecords succeeds without DPoP-Nonce header", async () => {
    const calls: string[] = [];
    const oauthSession = {
      did: "did:plc:test",
      getTokenInfo: async () => ({
        aud: "https://jellybaby.us-east.host.bsky.network",
      }),
      fetchHandler: async (url: string, init?: RequestInit) => {
        calls.push(`${init?.method ?? "GET"} ${url}`);
        return new Response(null, { status: 200 });
      },
      server: {
        dpopNonces: {
          get: async () => "cached-after-list",
          set: async () => {},
        },
      },
    };

    const nonce = await refreshPdsDpopNonceForGateway(
      oauthSession as never,
      "com.atproto.repo.listRecords",
      "GET"
    );

    expect(calls).toHaveLength(1);
    expect(calls[0]).toContain("com.atproto.repo.listRecords");
    expect(nonce).toBe("cached-after-list");
  });
});
