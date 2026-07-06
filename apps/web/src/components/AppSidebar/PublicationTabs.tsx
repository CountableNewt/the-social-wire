"use client";

import { Rss, Users } from "lucide-react";

import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import type { PublicationTab } from "./appSidebarConstants";

export function PublicationTabs({
  activeTab,
  onTabChange,
}: {
  activeTab: PublicationTab;
  onTabChange: (tab: PublicationTab) => void;
}) {
  return (
    <SidebarGroup className="pb-1 pt-1">
      <SidebarGroupLabel>Feeds</SidebarGroupLabel>
      <SidebarMenu className="gap-0.5" role="tablist" aria-label="Publication Source">
        <SidebarMenuItem>
          <SidebarMenuButton
            type="button"
            role="tab"
            aria-selected={activeTab === "subscribed"}
            isActive={activeTab === "subscribed"}
            onClick={() => onTabChange("subscribed")}
          >
            <Rss />
            <span>Subscribed</span>
          </SidebarMenuButton>
        </SidebarMenuItem>
        <SidebarMenuItem>
          <SidebarMenuButton
            type="button"
            role="tab"
            aria-selected={activeTab === "following"}
            isActive={activeTab === "following"}
            onClick={() => onTabChange("following")}
          >
            <Users />
            <span>Following</span>
          </SidebarMenuButton>
        </SidebarMenuItem>
      </SidebarMenu>
    </SidebarGroup>
  );
}
