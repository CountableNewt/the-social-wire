"use client";

import { SidebarMenuBadge } from "@/components/ui/sidebar";

export function UnreadSidebarBadge({ count }: { count: number }) {
  if (count <= 0) return null;
  const label = String(count);
  return (
    <SidebarMenuBadge aria-label={`${label} unread`}>{label}</SidebarMenuBadge>
  );
}
