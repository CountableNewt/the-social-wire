"use client";

import { useQuery } from "@tanstack/react-query";
import { Newspaper } from "lucide-react";
import type { ArticlePublication } from "@/lib/articles/articlePublishingTypes";
import { publicationIconUrlFromRecord } from "@/lib/atprotoClient";
import { Avatar } from "@/components/shared/Avatar";

export function ArticlePublicationIcon({ publication }: { publication?: ArticlePublication }) {
  const icon = useQuery({
    queryKey: ["articles", "publicationIcon", publication?.uri, publication?.cid],
    // XRPC decodes blob refs to CID objects; the shared resolver accepts their wire shape.
    queryFn: async () => publication ? await publicationIconUrlFromRecord(publication.uri, JSON.parse(JSON.stringify(publication.record))) ?? null : null,
    enabled: !!publication,
    staleTime: 30 * 60_000,
    retry: false,
  });
  return <span aria-hidden="true" className="flex size-6 shrink-0 items-center justify-center">
    {publication ? <Avatar src={icon.data} alt={publication.name} size={24} className="size-6" /> : <Newspaper className="size-4 text-muted-foreground" />}
  </span>;
}
