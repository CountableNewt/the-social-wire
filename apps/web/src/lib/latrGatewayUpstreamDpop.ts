import type { OAuthSession } from "@atproto/oauth-client-browser";
import {
  createUpstreamDpopProof,
  pdsXrpcMethodForGatewayRequest,
  primePdsDpopNonce,
  refreshPdsDpopNonce,
} from "latr-packages/gateway-client";

export {
  createUpstreamDpopProof,
  pdsXrpcMethodForGatewayRequest,
  primePdsDpopNonce,
  refreshPdsDpopNonce,
};

const LATR_SAVED_ITEM_COLLECTION = "com.latr.saved.item";

function pdsOrigin(pdsBase: string): string {
  return new URL(`${pdsBase.replace(/\/$/, "")}/`).origin;
}

/**
 * Advance the viewer PDS DPoP nonce chain without spurious createRecord probes
 * when listRecords succeeds but omits DPoP-Nonce (fetchHandler may still update
 * the OAuth nonce cache).
 */
export async function refreshPdsDpopNonceForGateway(
  oauthSession: OAuthSession,
  xrpcMethod: string,
  httpMethod: "GET" | "POST"
): Promise<string | undefined> {
  const tokenInfo = await oauthSession.getTokenInfo();
  const pdsBase = tokenInfo.aud.replace(/\/$/, "");
  const origin = pdsOrigin(pdsBase);

  const params = new URLSearchParams({
    repo: oauthSession.did,
    collection: LATR_SAVED_ITEM_COLLECTION,
    limit: "1",
  });
  const listRes = await oauthSession.fetchHandler(
    `${pdsBase}/xrpc/com.atproto.repo.listRecords?${params}`,
    { method: "GET" }
  );

  const headerNonce =
    listRes.headers.get("DPoP-Nonce") ?? listRes.headers.get("dpop-nonce");
  if (headerNonce?.trim()) {
    try {
      await oauthSession.server.dpopNonces.set(origin, headerNonce.trim());
    } catch {
      /* ignore cache write failures */
    }
    return headerNonce.trim();
  }

  if (listRes.ok) {
    try {
      const cached = await oauthSession.server.dpopNonces.get(origin);
      if (cached) return cached;
    } catch {
      /* ignore cache read failures */
    }
  }

  return refreshPdsDpopNonce(oauthSession, xrpcMethod, httpMethod);
}

/** Mint a PDS-bound upstream proof using the gateway-safe nonce refresh path. */
export async function createGatewayUpstreamDpopProof(
  oauthSession: OAuthSession,
  xrpcMethod: string,
  httpMethod: "GET" | "POST"
): Promise<string> {
  const pdsDpopNonce = await refreshPdsDpopNonceForGateway(
    oauthSession,
    xrpcMethod,
    httpMethod
  );
  return createUpstreamDpopProof(oauthSession, xrpcMethod, httpMethod, {
    pdsDpopNonce,
  });
}

/** Upstream proof pool for POST /v1/latr/saves (multi-record write-through). */
export async function createGatewaySaveUpstreamDpopProofPool(
  oauthSession: OAuthSession
): Promise<string> {
  const specs = [
    {
      xrpcMethod: "com.atproto.repo.createRecord",
      httpMethod: "POST" as const,
      count: 2,
    },
    {
      xrpcMethod: "com.atproto.repo.putRecord",
      httpMethod: "POST" as const,
      count: 2,
    },
  ];

  const proofs: string[] = [];
  for (const spec of specs) {
    for (let index = 0; index < spec.count; index += 1) {
      proofs.push(
        await createGatewayUpstreamDpopProof(
          oauthSession,
          spec.xrpcMethod,
          spec.httpMethod
        )
      );
    }
  }
  return proofs.join(",");
}

/** PDS XRPC method for Social Wire gateway routes that write through to the viewer PDS. */
export function pdsXrpcMethodForSocialWireGatewayRequest(
  gatewayMethod: string,
  gatewayPath: string
): { xrpcMethod: string; httpMethod: "GET" | "POST" } | null {
  const method = gatewayMethod.toUpperCase();
  const path = gatewayPath.startsWith("/") ? gatewayPath : `/${gatewayPath}`;

  if (method === "POST" && path === "/v1/appview/mark-all-read") {
    return { xrpcMethod: "com.atproto.repo.putRecord", httpMethod: "POST" };
  }
  return null;
}
