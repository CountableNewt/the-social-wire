/** Delegated Accounts stays dormant until explicitly enabled at build time. */
export function brandAccountsEnabled(): boolean {
  return process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED === "true";
}
