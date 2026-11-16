"use client";

import { Rows3 } from "lucide-react";
import { Avatar } from "@/components/shared/Avatar";
import {
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import type { SavedFeedSource } from "@/lib/savedFeedSources";

export function SavedFeedSourcesSection({
  sources,
  selectedSource,
  onSelectSource,
}: {
  sources: SavedFeedSource[];
  selectedSource: string | null;
  onSelectSource: (sourceKey: string | null) => void;
}) {
  return (
    <>
      <SidebarGroupLabel>Publications</SidebarGroupLabel>
      <SidebarMenu aria-label="Saved Feed Publications" className="gap-0.5">
        <SidebarMenuItem>
          <SidebarMenuButton
            type="button"
            isActive={!selectedSource}
            aria-current={!selectedSource ? "page" : undefined}
            onClick={() => onSelectSource(null)}
          >
            <Rows3 />
            <span>All Publications</span>
          </SidebarMenuButton>
        </SidebarMenuItem>
        {sources.map((source) => (
          <SidebarMenuItem key={source.key}>
            <SidebarMenuButton
              type="button"
              isActive={selectedSource === source.key}
              onClick={() => onSelectSource(source.key)}
              className="relative pr-8"
            >
              <Avatar
                src={source.faviconUrl}
                alt=""
                size={16}
                className="size-4 shrink-0"
              />
              <span className="truncate">{source.name}</span>
              <SidebarMenuBadge>{source.count}</SidebarMenuBadge>
            </SidebarMenuButton>
          </SidebarMenuItem>
        ))}
      </SidebarMenu>
    </>
  );
}
