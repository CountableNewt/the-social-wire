import { brandAccountsEnabled } from "@/lib/brandAccountsEnabled";
import { OAuthClient, type OAuthSession } from "@atproto/oauth-client-browser";
import { getOAuthClientForScopes, getSession, getStoredOAuthDid, localLoopbackCanonicalHref } from "@/lib/auth";
import { requirePro } from "@/lib/proEntitlements";
import { atprotoScopesForNetwork, authorizationScopesForClient } from "@/lib/atprotoNetwork";
import { observeOAuthScopeErrors } from "@/lib/oauthScopeRecovery";
import { BRAND_ACCOUNT_OAUTH_SCOPES } from "./brandAccountScopes";
import { BRAND_LINK_STATE_PREFIX, cancelBrandLinkIntent, createBrandLinkIntent, listBrandAccounts as storedBrandAccounts, removeBrandAccount } from "./brandAccountStorage";
export { onBrandAccountsChanged } from "./brandAccountStorage";
export function listBrandAccounts(ownerDid: string): string[] { return brandAccountsEnabled() ? storedBrandAccounts(ownerDid) : []; }
function requireOwner(ownerDid: string): void { if (!brandAccountsEnabled()) throw new Error("Delegated Accounts Are Not Available In This Environment."); if (getStoredOAuthDid() !== ownerDid) throw new Error("Your Personal Account Changed. Reopen Delegated Accounts."); }
async function requireOwnerPro(ownerDid: string): Promise<void> {
  requireOwner(ownerDid);
  const ownerSession = await getSession();
  requireOwner(ownerDid);
  if (!ownerSession || ownerSession.did !== ownerDid) throw new Error("Your Personal Account Changed. Reopen Delegated Accounts.");
  await requirePro(ownerSession);
  requireOwner(ownerDid);
}
export async function restoreBrandAccount(ownerDid: string, brandDid: string): Promise<OAuthSession> {
  requireOwner(ownerDid);
  if (brandDid === ownerDid || !listBrandAccounts(ownerDid).includes(brandDid)) throw new Error("This Delegated Account Is Not Linked To Your Personal Account.");
  await requireOwnerPro(ownerDid);
  const client = await getOAuthClientForScopes(BRAND_ACCOUNT_OAUTH_SCOPES); requireOwner(ownerDid);
  // BrowserOAuthClient.restore changes the global active-account hint. The public
  // core method restores the same SDK-managed DID session without switching it.
  const session = await OAuthClient.prototype.restore.call(client, brandDid);
  requireOwner(ownerDid);
  if (session.did !== brandDid || !listBrandAccounts(ownerDid).includes(brandDid)) throw new Error("This Delegated Account Changed Or Was Unlinked.");
  return observeOAuthScopeErrors(session);
}
export async function linkBrandAccount(ownerDid: string, handle: string): Promise<void> {
  requireOwner(ownerDid); if (!handle.trim() || handle.trim() === ownerDid) throw new Error("Enter A Different Delegated Account Handle Or DID.");
  await requireOwnerPro(ownerDid);
  const canonical = localLoopbackCanonicalHref(window.location.href);
  if (canonical) { window.location.replace(canonical); return; }
  const client = await getOAuthClientForScopes(BRAND_ACCOUNT_OAUTH_SCOPES); requireOwner(ownerDid);
  const intent = createBrandLinkIntent(ownerDid, client.clientMetadata.client_id);
  try { await client.signInRedirect(handle.trim(), { state: BRAND_LINK_STATE_PREFIX + intent.nonce, scope: authorizationScopesForClient(atprotoScopesForNetwork(BRAND_ACCOUNT_OAUTH_SCOPES), client.clientMetadata.client_id) }); }
  catch (error) { cancelBrandLinkIntent(); throw error; }
}
export async function unlinkBrandAccount(ownerDid: string, brandDid: string): Promise<void> {
  requireOwner(ownerDid);
  if (brandDid === ownerDid || !listBrandAccounts(ownerDid).includes(brandDid)) throw new Error("Only A Linked Delegated Account Can Be Unlinked.");
  // Disable its workspace before awaiting revocation. Its tokens stay inside the SDK.
  removeBrandAccount(ownerDid, brandDid);
  const client = await getOAuthClientForScopes(BRAND_ACCOUNT_OAUTH_SCOPES);
  await OAuthClient.prototype.revoke.call(client, brandDid);
}
