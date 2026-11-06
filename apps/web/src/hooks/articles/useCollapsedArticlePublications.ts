"use client";

import { useCallback, useEffect, useState } from "react";
import { loadCollapsedArticlePublications, saveCollapsedArticlePublications } from "@/lib/articles/articlePublicationCollapseStorage";

function browserStorage(): Storage | undefined {
  try { return typeof window === "undefined" ? undefined : window.localStorage; }
  catch { return undefined; }
}

/** Publication groups the viewer collapsed in the article library, shared by Drafts, Scheduled, and Published. */
export function useCollapsedArticlePublications(did: string) {
  const [collapsed, setCollapsed] = useState(() => {
    const storage = browserStorage();
    return new Set(storage ? loadCollapsedArticlePublications(storage, did) : []);
  });
  useEffect(() => {
    const storage = browserStorage();
    if (storage) saveCollapsedArticlePublications(storage, did, [...collapsed]);
  }, [did, collapsed]);
  const toggle = useCallback((id: string) => setCollapsed(current => {
    const next = new Set(current);
    if (!next.delete(id)) next.add(id);
    return next;
  }), []);
  const setAll = useCallback((ids: string[], isCollapsed: boolean) => setCollapsed(current => {
    const next = new Set(current);
    for (const id of ids) {
      if (isCollapsed) next.add(id);
      else next.delete(id);
    }
    return next;
  }), []);
  return { collapsed, toggle, setAll };
}
