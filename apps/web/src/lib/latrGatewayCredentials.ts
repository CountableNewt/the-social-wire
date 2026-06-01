import {
  LATR_API_KEY_HEADER,
  LATR_CLIENT_ID_HEADER,
} from "latr-packages/gateway-client";

const LATR_OFFICIAL_CLIENT_HEADER = "X-Latr-Official-Client";

/** True when the browser bundle can attach L@tr gateway client auth headers. */
export function hasLatrGatewayClientCredentials(): boolean {
  const clientId = process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_ID?.trim();
  const apiKey = process.env.NEXT_PUBLIC_LATR_GATEWAY_API_KEY?.trim();
  if (clientId && apiKey) return true;
  return Boolean(process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_CREDENTIAL?.trim());
}

export function latrGatewayCredentialsHelpText(): string {
  return (
    "Configure NEXT_PUBLIC_LATR_GATEWAY_CLIENT_ID and NEXT_PUBLIC_LATR_GATEWAY_API_KEY " +
    "(preferred, from latrkit.dev) or NEXT_PUBLIC_LATR_GATEWAY_CLIENT_CREDENTIAL on this deployment. " +
    "Testing builds use https://api.testing.latr.link — keys must be registered for that gateway."
  );
}

let authRejected = false;

/** After a 403 invalid_client_credential, skip further gateway enrichment calls this session. */
export function isLatrGatewayAuthRejected(): boolean {
  return authRejected;
}

export function markLatrGatewayAuthRejected(): void {
  authRejected = true;
}

export function resetLatrGatewayAuthRejectedForTests(): void {
  authRejected = false;
}

export function isLatrGatewayInvalidClientCredentialResponse(
  status: number,
  body: { error?: string; message?: string }
): boolean {
  if (status !== 403) return false;
  const code = body.error?.trim().toLowerCase();
  const message = body.message?.trim().toLowerCase() ?? "";
  return (
    code === "invalid_client_credential" ||
    message.includes("invalid gateway client credentials")
  );
}

export function latrGatewayClientAuthHeaderNames(): string[] {
  const clientId = process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_ID?.trim();
  const apiKey = process.env.NEXT_PUBLIC_LATR_GATEWAY_API_KEY?.trim();
  if (clientId && apiKey) {
    return [LATR_CLIENT_ID_HEADER, LATR_API_KEY_HEADER];
  }
  if (process.env.NEXT_PUBLIC_LATR_GATEWAY_CLIENT_CREDENTIAL?.trim()) {
    return [LATR_OFFICIAL_CLIENT_HEADER];
  }
  return [];
}
