import { toolsEnabled } from "@/lib/toolsEnabled";

/** Brand Accounts stays dormant until explicitly enabled at build time. */
export function brandAccountsEnabled(): boolean {
  return toolsEnabled() && process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED === "true";
}
