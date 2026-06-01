import { getAppEnv } from "@/lib/appEnv";

export const LOCAL_LATR_GATEWAY_URL = "http://127.0.0.1:8080";
export const DEFAULT_TEST_LATR_GATEWAY_URL = "https://api.testing.latr.link";
export const DEFAULT_DEV_LATR_GATEWAY_URL = DEFAULT_TEST_LATR_GATEWAY_URL;
export const DEFAULT_PROD_LATR_GATEWAY_URL = "https://api.latr.link";
/** Legacy Fly hostnames; override with `NEXT_PUBLIC_LATR_GATEWAY_URL` if needed. */
export const LEGACY_DEV_LATR_GATEWAY_URL =
  "https://latr-link-dev-gateway.fly.dev";
export const LEGACY_PROD_LATR_GATEWAY_URL =
  "https://latr-link-prod-gateway.fly.dev";

/** Base URL for L@tr / LatrKit gateway mutations from The Social Wire. */
export function latrGatewayBaseUrl(): string {
  const configured = process.env.NEXT_PUBLIC_LATR_GATEWAY_URL?.trim();
  if (configured) return configured.replace(/\/$/, "");

  switch (getAppEnv()) {
    case "prod":
      return DEFAULT_PROD_LATR_GATEWAY_URL;
    case "test":
    case "dev":
      return DEFAULT_DEV_LATR_GATEWAY_URL;
    default:
      return LOCAL_LATR_GATEWAY_URL;
  }
}
