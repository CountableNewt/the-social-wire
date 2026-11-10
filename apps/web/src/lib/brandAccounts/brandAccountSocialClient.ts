import { brandAccountsEnabled } from "@/lib/brandAccountsEnabled";
import { validateSocialPostRecord } from "@/lib/socialPostRecord";
import { compilePostText, resolvePostText } from "@/lib/composePostText";
import { ComAtprotoRepoStrongRef, type AppBskyFeedDefs } from "@atproto/api";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import { createOAuthAgent } from "@/lib/atprotoClient";
import { createAuthenticatedAppViewAgent } from "@/lib/blueskySocialClient";
import { composeExternalEmbed, type ComposeLinkPreview } from "@/lib/blueskyExternalEmbed";
import { postTextLength } from "@/lib/blueskyComposeClient";

export async function publishBrandPost(args: { session: OAuthSession; text: string; linkPreview?: ComposeLinkPreview; parent?: AppBskyFeedDefs.PostView; assertCurrentActor: () => void }) {
  const assertAvailable = () => { if (!brandAccountsEnabled()) throw new Error("Brand Accounts Are Not Available In This Environment."); args.assertCurrentActor(); };
  if (!brandAccountsEnabled()) throw new Error("Brand Accounts Are Not Available In This Environment.");
  const length = postTextLength(args.text);
  if (!compilePostText(args.text).text.trim() || length.graphemes > 300 || length.bytes > 3000) throw new Error("Posts Must Contain 1–300 Characters and At Most 3,000 UTF-8 Bytes.");
  let reply;
  if (args.parent) {
    const parent = { uri: args.parent.uri, cid: args.parent.cid };
    const record = validateSocialPostRecord(args.parent.record);
    if (!record.success || !ComAtprotoRepoStrongRef.validateMain(parent).success) throw new Error("The Reply Target Is Unavailable.");
    reply = { parent, root: record.value.reply?.root ?? parent };
    if (!ComAtprotoRepoStrongRef.validateMain(reply.root).success) throw new Error("The Reply Thread Is Invalid.");
  }
  assertAvailable();
  const compiled = await resolvePostText(args.text, createAuthenticatedAppViewAgent(args.session));
  const facets = compiled.facets;
  assertAvailable();
  const embed = await composeExternalEmbed(args.session, args.text, args.linkPreview, assertAvailable);
  assertAvailable();
  const result = await createOAuthAgent(args.session).post({ text: compiled.text, ...(facets?.length ? { facets } : {}), ...(reply ? { reply } : {}), ...(embed ? { embed } : {}) });
  assertAvailable();
  return result;
}
