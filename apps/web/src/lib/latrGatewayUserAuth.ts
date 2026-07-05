import type { OAuthSession } from "@atproto/oauth-client-browser";

type TokenSet = {
  access_token: string;
  token_type: string;
};

type SessionWithTokenSet = OAuthSession & {
  getTokenSet(refresh: boolean | "auto"): Promise<TokenSet>;
};

function stripQueryAndFragment(url: string): string {
  const fragmentIndex = url.indexOf("#");
  const queryIndex = url.indexOf("?");
  if (fragmentIndex === -1 && queryIndex === -1) return url;
  if (fragmentIndex === -1) return url.slice(0, queryIndex);
  if (queryIndex === -1) return url.slice(0, fragmentIndex);
  return url.slice(0, Math.min(fragmentIndex, queryIndex));
}

async function sha256Base64Url(input: string): Promise<string> {
  const bytes = new TextEncoder().encode(input);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  const view = new Uint8Array(digest);
  let binary = "";
  for (const byte of view) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function gatewayOrigin(gatewayUrl: string): string {
  return new URL(gatewayUrl).origin;
}

export function latrGatewayProxyAuthUrl(proxyUrl: string): string {
  if (/^https?:\/\//i.test(proxyUrl)) {
    return proxyUrl;
  }

  const origin =
    typeof globalThis.location?.origin === "string"
      ? globalThis.location.origin
      : "http://localhost";
  return new URL(proxyUrl, origin).toString();
}

async function readCachedGatewayNonce(
  oauthSession: OAuthSession,
  gatewayUrl: string
): Promise<string | undefined> {
  try {
    const cached = await oauthSession.server.dpopNonces.get(gatewayOrigin(gatewayUrl));
    return cached || undefined;
  } catch {
    return undefined;
  }
}

async function writeCachedGatewayNonce(
  oauthSession: OAuthSession,
  gatewayUrl: string,
  nonce: string
): Promise<void> {
  try {
    await oauthSession.server.dpopNonces.set(gatewayOrigin(gatewayUrl), nonce);
  } catch {
    /* ignore cache write failures */
  }
}

export async function captureGatewayDpopNonceFromResponse(
  oauthSession: OAuthSession,
  gatewayUrl: string,
  response: Response
): Promise<void> {
  const nonce =
    response.headers.get("DPoP-Nonce") ?? response.headers.get("dpop-nonce");
  if (nonce?.trim()) {
    await writeCachedGatewayNonce(oauthSession, gatewayUrl, nonce.trim());
  }
}

/** Mint Authorization + DPoP for the same-origin proxy URL the browser fetches. */
export async function buildLatrGatewayUserAuthHeaders(
  oauthSession: OAuthSession,
  method: string,
  proxyUrl: string,
  options: {
    authorizationScheme?: "Bearer" | "DPoP";
    dpopNonce?: string;
    includeQuery?: boolean;
    useCachedNonce?: boolean;
  } = {}
): Promise<Record<string, string>> {
  const tokenSet = await (oauthSession as SessionWithTokenSet).getTokenSet("auto");
  const authorization = `${
    options.authorizationScheme ?? tokenSet.token_type
  } ${tokenSet.access_token}`;
  const ath = await sha256Base64Url(tokenSet.access_token);
  const htu = options.includeQuery
    ? proxyUrl.split("#", 1)[0] ?? proxyUrl
    : stripQueryAndFragment(proxyUrl);
  const nonce =
    options.dpopNonce ??
    (options.useCachedNonce === false
      ? undefined
      : await readCachedGatewayNonce(oauthSession, proxyUrl));

  const key = oauthSession.server.dpopKey;
  const jwk = key.bareJwk;
  if (!jwk) {
    throw new Error("OAuth session DPoP key is unavailable");
  }

  const supported =
    oauthSession.server.serverMetadata.dpop_signing_alg_values_supported;
  const alg =
    supported?.find((candidate) => key.algorithms.includes(candidate)) ??
    key.algorithms[0];
  if (!alg) {
    throw new Error("OAuth session DPoP key has no supported algorithm");
  }

  const now = Math.floor(Date.now() / 1000);
  const claims: Record<string, string | number> = {
    iat: now,
    jti: Math.random().toString(36).slice(2),
    htm: method.toUpperCase(),
    htu,
    ath,
    ...(nonce ? { nonce } : {}),
  };

  const dpop = await key.createJwt({ alg, typ: "dpop+jwt", jwk }, claims);
  return {
    Authorization: authorization,
    DPoP: dpop,
  };
}
