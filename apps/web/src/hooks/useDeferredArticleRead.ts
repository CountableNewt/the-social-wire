"use client";

import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

/** Keep the open article available until the viewer moves on within TSW. */
export function useDeferredArticleRead({ scope, viewerKey, markRead }: {
  scope: string;
  viewerKey: string;
  markRead: (entryId: string) => void;
}) {
  const currentViewer = useRef(viewerKey);
  const selection = useRef<{ entryId: string; scope: string; viewerKey: string } | null>(null);
  const [selected, setSelected] = useState<typeof selection.current>(null);
  const pending = useRef<{ entryId: string; viewerKey: string; markRead: (entryId: string) => void; isCurrent?: () => boolean } | null>(null);
  useLayoutEffect(() => { currentViewer.current = viewerKey; }, [viewerKey]);

  const finish = useCallback(() => {
    const opened = pending.current;
    pending.current = null;
    selection.current = null;
    setSelected(null);
    if (opened?.viewerKey === currentViewer.current && (!opened.isCurrent || opened.isCurrent())) opened.markRead(opened.entryId);
  }, []);
  const open = useCallback((entryId: string, isCurrent?: () => boolean) => {
    if (pending.current?.entryId === entryId && pending.current.viewerKey === viewerKey) return;
    finish();
    pending.current = { entryId, viewerKey, markRead, isCurrent };
    selection.current = { entryId, scope, viewerKey };
    setSelected(selection.current);
  }, [finish, markRead, scope, viewerKey]);
  const cancel = useCallback((entryId: string) => {
    if (pending.current?.entryId === entryId) pending.current = null;
  }, []);

  // Callback changes and tab visibility do not mean the reader navigated away.
  useEffect(() => () => finish(), [finish, scope, viewerKey]);
  useEffect(() => {
    window.addEventListener("pagehide", finish);
    return () => window.removeEventListener("pagehide", finish);
  }, [finish]);
  useEffect(() => {
    const onClick = (event: MouseEvent) => {
      const opened = selection.current;
      if (!opened) return;
      const inside = event.composedPath().some(node => node instanceof window.Element &&
        (node.getAttribute("data-entry-id") === opened.entryId || node.getAttribute("data-entry-owner") === opened.entryId));
      if (inside) return;
      // Let the clicked action finish before an unread row can be removed.
      // Another card may have become selected during the same click.
      queueMicrotask(() => { if (selection.current === opened) finish(); });
    };
    document.addEventListener("click", onClick, true);
    return () => document.removeEventListener("click", onClick, true);
  }, [finish]);
  const selectedEntryId = selected?.viewerKey === viewerKey && selected.scope === scope ? selected.entryId : null;
  return { open, finish, cancel, selectedEntryId };
}
