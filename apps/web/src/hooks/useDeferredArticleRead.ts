"use client";

import { useCallback, useEffect, useLayoutEffect, useRef } from "react";

/** Keep the open article available until the viewer moves on within TSW. */
export function useDeferredArticleRead({ scope, viewerKey, markRead }: {
  scope: string;
  viewerKey: string;
  markRead: (entryId: string) => void;
}) {
  const currentViewer = useRef(viewerKey);
  const pending = useRef<{ entryId: string; viewerKey: string; markRead: (entryId: string) => void; isCurrent?: () => boolean } | null>(null);
  useLayoutEffect(() => { currentViewer.current = viewerKey; }, [viewerKey]);

  const finish = useCallback(() => {
    const opened = pending.current;
    pending.current = null;
    if (opened?.viewerKey === currentViewer.current && (!opened.isCurrent || opened.isCurrent())) opened.markRead(opened.entryId);
  }, []);
  const open = useCallback((entryId: string, isCurrent?: () => boolean) => {
    if (pending.current?.entryId === entryId && pending.current.viewerKey === viewerKey) return;
    finish();
    pending.current = { entryId, viewerKey, markRead, isCurrent };
  }, [finish, markRead, viewerKey]);
  const cancel = useCallback((entryId: string) => {
    if (pending.current?.entryId === entryId) pending.current = null;
  }, []);

  // Callback changes and tab visibility do not mean the reader navigated away.
  useEffect(() => () => finish(), [finish, scope, viewerKey]);
  useEffect(() => {
    window.addEventListener("pagehide", finish);
    return () => window.removeEventListener("pagehide", finish);
  }, [finish]);
  return { open, finish, cancel };
}
