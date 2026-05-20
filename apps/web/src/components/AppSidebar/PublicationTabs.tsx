"use client";

import { SidebarMenuItem, SIDEBAR_GLASS_SEGMENTED } from "@/components/ui/sidebar";
import { cn } from "@/lib/utils";
import type { PublicationTab } from "./appSidebarConstants";
import { PublicationTabButton } from "./PublicationTabButton";

export function PublicationTabs({
  activeTab,
  onTabChange,
}: {
  activeTab: PublicationTab;
  onTabChange: (tab: PublicationTab) => void;
}) {
  return (
    <SidebarMenuItem>
      <div
        className={cn(SIDEBAR_GLASS_SEGMENTED)}
        role="tablist"
        aria-label="Publication Source"
      >
        <PublicationTabButton
          active={activeTab === "subscribed"}
          onClick={() => onTabChange("subscribed")}
        >
          Subscribed
        </PublicationTabButton>
        <PublicationTabButton
          active={activeTab === "following"}
          onClick={() => onTabChange("following")}
        >
          Following
        </PublicationTabButton>
      </div>
    </SidebarMenuItem>
  );
}
