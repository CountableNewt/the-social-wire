"use client";

import { useAuth } from "@/hooks/useAuth";
import type { EntryListItem } from "@/lib/atprotoClient";
import { LazyArticleCardActions } from "./LazyArticleCardActions";

export function EntryCardActionMenu({ entry }: { entry: EntryListItem }) {
  const { session, oauthSessionReloadSeq } = useAuth();
  return <LazyArticleCardActions key={`${session?.did ?? "public"}:${oauthSessionReloadSeq}:${entry.entryId}`} entry={entry} />;
}
