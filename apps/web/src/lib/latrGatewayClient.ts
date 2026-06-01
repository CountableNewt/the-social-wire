import type { OAuthSession } from "@atproto/oauth-client-browser";
import {
  buildDeveloperGatewayHeaders,
  LATR_API_KEY_HEADER,
  LATR_CLIENT_ID_HEADER,
  LATR_UPSTREAM_DPOP_HEADER,
} from "latr-packages/gateway-client";

import {
  hasLatrGatewayClientCredentials,
  isLatrGatewayAuthRejected,
  isLatrGatewayInvalidClientCredentialResponse,
  latrGatewayCredentialsHelpText,
  markLatrGatewayAuthRejected,
} from "@/lib/latrGatewayCredentials";
import {
  createSaveUpstreamDpopProofPool,
  createUpstreamDpopProof,
  pdsXrpcMethodForGatewayRequest,
} from "@/lib/latrGatewayUpstreamDpop";
import { latrGatewayBaseUrl } from "@/lib/latrGatewayUrl";

/** Legacy official first-party credential header (internal apps during migration). */
export const LATR_OFFICIAL_CLIENT_HEADER = "X-Latr-Official-Client";

export {
  LATR_API_KEY_HEADER,
  LATR_CLIENT_ID_HEADER,
  LATR_UPSTREAM_DPOP_HEADER,
};

export { latrGatewayBaseUrl } from "@/lib/latrGatewayUrl";

function shouldRetryLatrGatewayDpopNonce(res: Response): boolean {
  if (res.status !== 401 && res.status !== 400) return false;
  return Boolean(res.headers.get("DPoP-Nonce")?.trim());
}

async function buildUpstreamDpopHeader(
  oauthSession: OAuthSession,
  method: string,
  gatewayPath: string
): Promise<string | undefined> {
  if (method === "POST" && gatewayPath === "/v1/latr/saves") {
    return createSaveUpstreamDpopProofPool(oauthSession);
  }

  const upstream = pdsXrpcMethodForGatewayRequest(method, gatewayPath);
  if (!upstream) return undefined;

  return createUpstreamDpopProof(
    oauthSession,
    upstream.xrpcMethod,
    upstream.httpMethod
  );
}

async function buildLatrGatewayRequestHeaders(
  oauthSession: OAuthSession,
  method: string,
  gatewayPath: string
): Promise<Record<string, string>> {
  const clientId = process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_ID?.trim();
  const apiKey = process.env.NEXT_PUBLIC_LATR_GATEWAY_API_KEY?.trim();
  const clientCredential = process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_CREDENTIAL?.trim();
  const headers: Record<string, string> = {
    Accept: "application/json",
  };
  if (clientId && apiKey) {
    Object.assign(headers, buildDeveloperGatewayHeaders({ clientId, apiKey }));
  } else if (clientCredential) {
    headers[LATR_OFFICIAL_CLIENT_HEADER] = clientCredential;
  }

  const upstreamProof = await buildUpstreamDpopHeader(
    oauthSession,
    method,
    gatewayPath
  );
  if (upstreamProof) {
    headers[LATR_UPSTREAM_DPOP_HEADER] = upstreamProof;
  }
  return headers;
}

export async function latrGatewayFetch(
  oauthSession: OAuthSession,
  path: string,
  init?: RequestInit,
  attempt = 0
): Promise<Response> {
  if (!hasLatrGatewayClientCredentials()) {
    return new Response(
      JSON.stringify({
        error: "missing_client_credential",
        message: latrGatewayCredentialsHelpText(),
      }),
      { status: 403, headers: { "Content-Type": "application/json" } }
    );
  }

  const gatewayPath = path.startsWith("/") ? path : `/${path}`;
  const url = `${latrGatewayBaseUrl()}${gatewayPath}`;
  const method = init?.method ?? "GET";
  const baseHeaders = await buildLatrGatewayRequestHeaders(
    oauthSession,
    method,
    gatewayPath
  );

  const res = await oauthSession.fetchHandler(url, {
    ...init,
    headers: {
      ...baseHeaders,
      ...(init?.headers ?? {}),
    },
  });

  if (attempt === 0 && shouldRetryLatrGatewayDpopNonce(res)) {
    return latrGatewayFetch(oauthSession, path, init, attempt + 1);
  }

  await noteInvalidClientCredential(res);
  return res;
}

async function noteInvalidClientCredential(res: Response): Promise<void> {
  if (isLatrGatewayAuthRejected()) return;
  try {
    const body = (await res.clone().json()) as { message?: string; error?: string };
    if (isLatrGatewayInvalidClientCredentialResponse(res.status, body)) {
      markLatrGatewayAuthRejected();
    }
  } catch {
    /* ignore parse failures */
  }
}

async function readGatewayError(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { message?: string; error?: string };
    if (isLatrGatewayInvalidClientCredentialResponse(res.status, body)) {
      return latrGatewayCredentialsHelpText();
    }
    if (body.error === "missing_client_credential") {
      return latrGatewayCredentialsHelpText();
    }
    return body.message ?? body.error ?? `Gateway error (${res.status})`;
  } catch {
    return `Gateway error (${res.status})`;
  }
}

export async function latrGatewayJson<T>(
  oauthSession: OAuthSession,
  path: string,
  init?: RequestInit
): Promise<T> {
  const res = await latrGatewayFetch(oauthSession, path, init);
  if (!res.ok) {
    throw new Error(await readGatewayError(res));
  }
  return (await res.json()) as T;
}
