"use client";

import { SidebarMenuBadge } from "@/components/ui/sidebar";

export function readLaterSavedCountLabel(count: number): string {
  return count === 1 ? "1 saved link" : `${count} saved links`;
}

export function ReadLaterSidebarBadge({ count }: { count: number }) {
  if (count <= 0) return null;

  return (
    <SidebarMenuBadge aria-label={readLaterSavedCountLabel(count)}>
      {count}
    </SidebarMenuBadge>
  );
}
