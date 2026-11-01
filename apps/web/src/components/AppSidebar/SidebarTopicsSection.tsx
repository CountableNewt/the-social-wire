"use client";

import { ChartNoAxesCombined, Trophy } from "lucide-react";

import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { WireBetaBadge } from "@/components/Wire/WireBetaBadge";
import type { ReaderNavigationFeed } from "@/lib/feedPreferences";
import { CompactTopicInterests } from "./CompactTopicInterests";

export function SidebarTopicsSection({
  visibleFeeds,
  sportsEnabled = false,
  sportsActive = false,
  onSportsSelect,
  financeActive = false,
  onFinanceSelect,
  compact = false,
  onNavigate,
}: {
  visibleFeeds?: ReadonlySet<ReaderNavigationFeed>;
  sportsEnabled?: boolean;
  sportsActive?: boolean;
  onSportsSelect?: () => void;
  financeActive?: boolean;
  onFinanceSelect?: () => void;
  compact?: boolean;
  onNavigate?: (href: string) => void;
}) {
  if (visibleFeeds?.has("finance") === false && (!sportsEnabled || visibleFeeds?.has("sports") === false)) return null;

  return (
    <SidebarGroup className="pb-1 pt-1">
      <SidebarGroupLabel>Topics</SidebarGroupLabel>
      <SidebarMenu className="gap-0.5" role={compact ? undefined : "tablist"} aria-label="Topics">
        {visibleFeeds?.has("finance") !== false ? <SidebarMenuItem>
          <SidebarMenuButton
            type="button"
            role={compact ? undefined : "tab"}
            aria-label="Finance, Beta"
            aria-selected={compact ? undefined : financeActive}
            aria-current={compact && financeActive ? "page" : undefined}
            isActive={financeActive}
            onClick={onFinanceSelect}
          >
            <ChartNoAxesCombined />
            <span>Finance</span>
            <WireBetaBadge className="ml-auto" />
          </SidebarMenuButton>
          {compact ? <CompactTopicInterests topic="finance" onNavigate={onNavigate} /> : null}
        </SidebarMenuItem> : null}
        {sportsEnabled && visibleFeeds?.has("sports") !== false ? <SidebarMenuItem><SidebarMenuButton type="button" role={compact ? undefined : "tab"} aria-label="Sports, Beta" aria-selected={compact ? undefined : sportsActive} aria-current={compact && sportsActive ? "page" : undefined} isActive={sportsActive} onClick={onSportsSelect}><Trophy /><span>Sports</span><WireBetaBadge className="ml-auto" /></SidebarMenuButton>{compact ? <CompactTopicInterests topic="sports" onNavigate={onNavigate} /> : null}</SidebarMenuItem> : null}
      </SidebarMenu>
    </SidebarGroup>
  );
}
