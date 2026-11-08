"use client";

import { SidebarGroup, SidebarGroupLabel } from "@/components/ui/sidebar";

export function SavedQuickStatsSection({
  unreadCount,
  archiveCount,
}: {
  unreadCount: number;
  archiveCount: number;
}) {
  return (
    <SidebarGroup className="p-0 pb-3">
      <SidebarGroupLabel>Quick Stats</SidebarGroupLabel>
      <dl aria-label="Read Later Quick Stats" className="flex flex-col gap-1.5 px-2 text-sm">
        <div className="flex items-center justify-between">
          <dt className="text-muted-foreground">Unread</dt>
          <dd className="font-medium tabular-nums text-primary">{unreadCount}</dd>
        </div>
        <div className="flex items-center justify-between">
          <dt className="text-muted-foreground">Archive</dt>
          <dd className="font-medium tabular-nums text-sidebar-foreground">{archiveCount}</dd>
        </div>
      </dl>
    </SidebarGroup>
  );
}
