import { assertArticleDraftActor } from "./articleDraftCloudClient";
import type { ArticleDraftActor } from "./articleDraftCloudTypes";
import { getArticleDraft, saveArticleDraft } from "./articleDraftStorage";
import { assertArticlePublicationEditable } from "./articlePublicationCapabilities";
import type { ArticlePublication } from "./articlePublishingTypes";

/** Starts a new private journal entry; public publication is always a separate action. */
export async function createArticleDraftFromLink(actor: ArticleDraftActor, publication: ArticlePublication, sourceUrl: string) {
  assertArticleDraftActor(actor);
  assertArticlePublicationEditable(publication);
  const prefix = `at://${actor.viewerDid}/site.standard.publication/`;
  if (!publication.uri.startsWith(prefix) || !/^[a-zA-Z0-9._~:-]+$/.test(publication.uri.slice(prefix.length))) throw new Error("Choose One of Your Publications.");
  const url = new URL(sourceUrl);
  if (url.protocol !== "https:" || url.username || url.password) throw new Error("This Article Has No Valid Site Link.");
  const id = crypto.randomUUID(), now = new Date().toISOString();
  if (await getArticleDraft(actor.viewerDid, id)) throw new Error("A Draft Already Exists With This ID. Try Again.");
  assertArticleDraftActor(actor);
  const draft = await saveArticleDraft(actor.viewerDid, { id, title: "", markdown: `${url.href}\n\n`, excerpt: "", path: "", tags: [], publicationUri: publication.uri, assets: [], createdAt: now, updatedAt: now, revision: 0 });
  assertArticleDraftActor(actor);
  return draft;
}
