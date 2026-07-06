"use client";

import { useId, type ReactNode } from "react";
import { SidebarMenuItem, SidebarMenuSub } from "@/components/ui/sidebar";
import type { DiscoveredPublication } from "@/lib/atprotoClient";
import type { GatewayMarkAllReadScope } from "@/lib/publicationProjectionClient";
import { SidebarReadBulkMenuWrap } from "./SidebarReadBulkMenuWrap";

function SectionUnreadBadge({ count }: { count: number }) {
  if (count <= 0) return null;
  return (
    <span
      className="ml-auto inline-flex h-5 min-w-5 items-center justify-center rounded-lg bg-primary/10 px-1 text-xs font-bold text-[var(--purple-foreground)] tabular-nums"
      aria-label={`${count} unread`}
    >
      {count}
    </span>
  );
}

export function CollapsibleSidebarSubSection({
  title,
  unreadCount = 0,
  subAriaLabel,
  readBulkPublications,
  readBulkMarkAllReadConfirmation,
  gatewayMarkAllReadScopes,
  children,
}: {
  title: string;
  unreadCount?: number;
  subAriaLabel: string;
  readBulkPublications?: DiscoveredPublication[];
  /** Required when `readBulkPublications` is provided */
  readBulkMarkAllReadConfirmation?: ReactNode;
  gatewayMarkAllReadScopes?: GatewayMarkAllReadScope[];
  children: ReactNode;
}) {
  const subId = `sidebar-collapsible-sub-${useId().replace(/:/g, "")}`;

  const sectionHeader = (
    <div
      className="flex h-6 w-full min-w-0 items-center gap-2 pl-2 pr-1 text-xs font-medium text-sidebar-foreground/70"
      aria-controls={subId}
    >
      <span className="min-w-0 flex-1 truncate">
        {title}
      </span>
      <SectionUnreadBadge count={unreadCount} />
    </div>
  );

  return (
    <SidebarMenuItem>
      {readBulkPublications !== undefined &&
      readBulkMarkAllReadConfirmation !== undefined ? (
        <SidebarReadBulkMenuWrap
          publications={readBulkPublications}
          markAllReadConfirmation={readBulkMarkAllReadConfirmation}
          gatewayScopes={gatewayMarkAllReadScopes}
        >
          {sectionHeader}
        </SidebarReadBulkMenuWrap>
      ) : (
        sectionHeader
      )}
      <SidebarMenuSub id={subId} aria-label={subAriaLabel}>
        {children}
      </SidebarMenuSub>
    </SidebarMenuItem>
  );
}
