"use client";

import type { ReaderNavigationFeed } from "@/lib/feedPreferences";
import { Clapperboard, Images } from "lucide-react";
import { SidebarGroup, SidebarGroupLabel, SidebarMenu, SidebarMenuItem, SidebarMenuButton } from "@/components/ui/sidebar";

export function SidebarAudioSection({ mediaEnabled = false, mediaActive, onSelectMedia }: { mediaEnabled?: boolean; mediaActive?: ReaderNavigationFeed | null; onSelectMedia?: (feed: "shortVideos" | "photos") => void }) {
  if (!mediaEnabled) return null;
  return <SidebarGroup className="pb-1 pt-1">
    <SidebarGroupLabel>Media</SidebarGroupLabel>
    <SidebarMenu role="tablist" aria-label="Media">
      {([{ feed: "shortVideos", label: "Short Videos", icon: Clapperboard }, { feed: "photos", label: "Photos", icon: Images }] as const).map(({ feed, label, icon: Icon }) => <SidebarMenuItem key={feed}><SidebarMenuButton type="button" role="tab" aria-selected={mediaActive === feed} isActive={mediaActive === feed} onClick={() => onSelectMedia?.(feed)} className="pointer-coarse:min-h-11"><Icon aria-hidden="true" /><span>{label}</span></SidebarMenuButton></SidebarMenuItem>)}
    </SidebarMenu>
  </SidebarGroup>;
}
