"use client";

import { ArticleSocialToolbar } from "@/components/EntryDetail/ArticleSocialToolbar";
import type { EntryDetail } from "@/lib/atprotoClient";

interface EntrySocialToolbarProps {
  entry: EntryDetail;
  className?: string;
}

export function EntrySocialToolbar({ entry, className }: EntrySocialToolbarProps) {
  return <ArticleSocialToolbar entry={entry} className={className} />;
}
