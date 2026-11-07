"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ImageIcon } from "lucide-react";
import type { ArticleDraft } from "@/lib/articles/articleDraftTypes";
import type { PublishedArticle } from "@/lib/articles/articlePublishingTypes";
import { getArticleDraftAsset } from "@/lib/articles/articleDraftStorage";
import { resolveEntryThumbnailUrls, type ResolvedEntryThumbnail } from "@/lib/atprotoClient";
import { CachedImage } from "@/components/shared/CachedImage";

export function ArticleLibraryThumbnail({ did, draft, article }: { did: string; draft?: ArticleDraft; article?: PublishedArticle }) {
  const draftId = draft?.id, assetId = draft?.coverAssetId;
  const key = JSON.stringify([did, draftId, assetId]);
  const [local, setLocal] = useState<{ key: string; url: string }>();
  const [failed, setFailed] = useState<Set<string>>(() => new Set());
  useEffect(() => {
    if (!draftId || !assetId) return;
    let canceled = false;
    let url: string | undefined;
    void getArticleDraftAsset(did, draftId, assetId).then(blob => {
      if (!blob || canceled) return;
      url = URL.createObjectURL(blob);
      setLocal({ key, url });
    }).catch(() => { /* A missing private cover must not block opening its draft. */ });
    return () => { canceled = true; if (url) URL.revokeObjectURL(url); };
  }, [did, draftId, assetId, key]);
  const thumbnail = useQuery<ResolvedEntryThumbnail>({
    queryKey: ["articles", did, "thumbnail", article?.uri, article?.cid],
    queryFn: () => {
      // XRPC decodes blob refs to CID objects; the shared resolver accepts their wire shape.
      return article ? resolveEntryThumbnailUrls(article.uri, JSON.parse(JSON.stringify(article.record))) : Promise.resolve({});
    },
    enabled: !!article,
    staleTime: 5 * 60_000,
    retry: false,
  });
  const localUrl = local?.key === key ? local.url : undefined;
  const remoteUrl = [thumbnail.data?.thumbnailUrl, thumbnail.data?.thumbnailFallbackUrl].find(url => url && !failed.has(url));
  const source = draft ? localUrl : remoteUrl;
  const className = "h-full w-full object-cover";
  return <span aria-hidden="true" className="relative flex h-16 w-20 shrink-0 items-center justify-center overflow-hidden rounded-lg bg-muted sm:h-20 sm:w-28">
    <ImageIcon className="absolute size-6 text-muted-foreground/50" />
    {source && !failed.has(source) ? draft ?
      /* eslint-disable-next-line @next/next/no-img-element -- Private IndexedDB cover preserves the original image format. */
      <img src={source} alt="" className={`relative ${className}`} onError={() => setFailed(previous => new Set(previous).add(source))} /> :
      <CachedImage src={source} alt="" width={112} height={80} className={`relative ${className}`} onError={() => setFailed(previous => new Set(previous).add(source))} /> : null}
  </span>;
}
