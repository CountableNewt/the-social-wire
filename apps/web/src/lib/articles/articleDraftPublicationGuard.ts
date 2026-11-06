import type { ArticleDraft } from "./articleDraftTypes";

/** Publication must use the same content that won private draft synchronization. */
export function sameArticleDraftContent(a: ArticleDraft, b: ArticleDraft): boolean {
  const content = (draft: ArticleDraft) => ({
    id: draft.id, createdAt: draft.createdAt, title: draft.title, markdown: draft.markdown,
    excerpt: draft.excerpt, path: draft.path, tags: draft.tags, publicationUri: draft.publicationUri,
    coverAssetId: draft.coverAssetId,
    assets: (draft.assets ?? []).map(asset => ({ id: asset.id, alt: asset.alt, width: asset.width, height: asset.height, mimeType: asset.mimeType, name: asset.name })),
  });
  return JSON.stringify(content(a)) === JSON.stringify(content(b));
}
