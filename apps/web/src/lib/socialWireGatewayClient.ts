import type { OAuthSession } from "@atproto/oauth-client-browser";
import { LATR_UPSTREAM_DPOP_HEADER } from "latr-packages/gateway-client";

import {
  createUpstreamDpopProof,
  pdsXrpcMethodForSocialWireGatewayRequest,
} from "@/lib/latrGatewayUpstreamDpop";

export function gatewayBaseUrl(): string {
  return (
    process.env.NEXT_PUBLIC_SOCIALWIRE_API_URL ?? "https://api.thesocialwire.app"
  ).replace(/\/$/, "");
}

export async function gatewayFetch(
  oauthSession: OAuthSession,
  path: string,
  init?: RequestInit
): Promise<Response> {
  const gatewayPath = path.startsWith("/") ? path : `/${path}`;
  const url = `${gatewayBaseUrl()}${gatewayPath}`;
  const method = init?.method ?? "GET";
  const upstreamHeaders: Record<string, string> = {};
  const upstream = pdsXrpcMethodForSocialWireGatewayRequest(method, gatewayPath);
  if (upstream) {
    upstreamHeaders[LATR_UPSTREAM_DPOP_HEADER] = await createUpstreamDpopProof(
      oauthSession,
      upstream.xrpcMethod,
      upstream.httpMethod
    );
  }

  return oauthSession.fetchHandler(url, {
    ...init,
    headers: {
      Accept: "application/json",
      ...upstreamHeaders,
      ...(init?.headers ?? {}),
    },
  });
}
