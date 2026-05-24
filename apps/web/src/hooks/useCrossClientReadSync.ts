"use client";

import { useEffect } from "react";
import { useReadRoute } from "@/contexts/ReadRouteContext";
import { usePublicationSidebarData } from "@/hooks/usePublicationSidebarData";

/** Pull PDS read markers and AppView unread baselines when the tab becomes visible again. */
export function useCrossClientReadSync() {
  const { syncReadStateFromPDS } = useReadRoute();
  const { refreshUnreadCountsFromAppView } = usePublicationSidebarData();

  useEffect(() => {
    if (typeof document === "undefined") return;

    const onVisible = () => {
      if (document.visibilityState !== "visible") return;
      void syncReadStateFromPDS();
      void refreshUnreadCountsFromAppView();
    };

    document.addEventListener("visibilitychange", onVisible);
    return () => document.removeEventListener("visibilitychange", onVisible);
  }, [syncReadStateFromPDS, refreshUnreadCountsFromAppView]);
}
