import type { ArticlePublication } from "./articlePublishingTypes";

export const PCKT_EXTERNAL_EDITORS_MESSAGE = "pckt Does Not Support External Editors.";

export function articlePublicationDisabledReason(publication: Pick<ArticlePublication, "host" | "url" | "record">): string | undefined {
  const pcktURL = [publication.url, publication.record.url].some(value => {
    if (typeof value !== "string") return false;
    try {
      const hostname = new URL(value).hostname.toLowerCase();
      return hostname === "pckt.blog" || hostname.endsWith(".pckt.blog");
    } catch { return false; }
  });
  return publication.host === "pckt" || pcktURL || JSON.stringify(publication.record.theme ?? "").toLowerCase().includes("pckt") ? PCKT_EXTERNAL_EDITORS_MESSAGE : undefined;
}

export function assertArticlePublicationEditable(publication: Pick<ArticlePublication, "host" | "url" | "record">): void {
  const reason = articlePublicationDisabledReason(publication);
  if (reason) throw new Error(reason);
}
