"use client";

import { useEffect } from "react";
import { useSidebarBootstrap } from "@/contexts/PublicationSidebarContext";

/** Refresh AppView unread baselines when the tab becomes visible again. */
export function useCrossClientReadSync(enabled = true) {
  const { refreshUnreadCountsFromAppView } = useSidebarBootstrap();

  useEffect(() => {
    if (!enabled) return;
    if (typeof document === "undefined") return;

    const onVisible = () => {
      if (document.visibilityState !== "visible") return;
      void refreshUnreadCountsFromAppView();
    };

    document.addEventListener("visibilitychange", onVisible);
    return () => document.removeEventListener("visibilitychange", onVisible);
  }, [enabled, refreshUnreadCountsFromAppView]);
}
