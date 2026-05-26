"use client";

import { ArticleSocialToolbar } from "@/components/EntryDetail/ArticleSocialToolbar";
import { Skeleton } from "@/components/ui/skeleton";
import { useSavedLinkSocialEntry } from "@/hooks/useSavedLinkSocialEntry";
import type { MergedLatrSave } from "@/lib/pdsClient";
import { cn } from "@/lib/utils";

type Props = {
  row: MergedLatrSave;
  className?: string;
};

export function SavedLinkSocialToolbar({ row, className }: Props) {
  const { entry, isLoading } = useSavedLinkSocialEntry(row);

  if (isLoading) {
    return (
      <div className={cn("mb-2", className)}>
        <Skeleton className="h-9 w-full max-w-md rounded-md" />
      </div>
    );
  }

  return (
    <ArticleSocialToolbar
      entry={entry}
      showReadLaterSave={false}
      className={className}
    />
  );
}
