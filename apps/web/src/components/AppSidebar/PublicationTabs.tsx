"use client";

import type { ReactNode } from "react";
import { Network, Newspaper, Rss, Users } from "lucide-react";

import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
} from "@/components/ui/sidebar";
import type { DiscoveredPublication } from "@/lib/atprotoClient";
import type { ReaderNavigationFeed } from "@/lib/feedPreferences";
import type { PublicationTab } from "./appSidebarConstants";
import { SidebarSectionUnreadBadge } from "./SidebarSectionUnreadBadge";
import { SidebarReadBulkMenuWrap } from "./SidebarReadBulkMenuWrap";
import { WireBetaBadge } from "@/components/Wire/WireBetaBadge";

export function PublicationTabs({
  subscribedChildren,
  followingChildren,
  visibleFeeds,
  activeTab,
  onTabChange,
  subscribedUnread = 0,
  followingUnread = 0,
  showSubscribedUnreadCount = true,
  showFollowingUnreadCount = true,
  subscribedPublications = [],
  followingPublications = [],
  wireActive = false,
  onWireSelect,
  circleActive = false,
  onCircleSelect,
}: {
  subscribedChildren?: ReactNode;
  followingChildren?: ReactNode;
  visibleFeeds?: ReadonlySet<ReaderNavigationFeed>;
  activeTab: PublicationTab | null;
  onTabChange: (tab: PublicationTab) => void;
  subscribedUnread?: number;
  followingUnread?: number;
  showSubscribedUnreadCount?: boolean;
  showFollowingUnreadCount?: boolean;
  subscribedPublications?: DiscoveredPublication[];
  followingPublications?: DiscoveredPublication[];
  wireActive?: boolean;
  onWireSelect?: () => void;
  circleActive?: boolean;
  onCircleSelect?: () => void;
}) {
  const nested = !!(subscribedChildren || followingChildren);
  if (
    visibleFeeds &&
    !["wire", "circle", "subscribed", "following"].some((feed) =>
      visibleFeeds.has(feed as ReaderNavigationFeed),
    )
  )
    return null;
  return (
    <SidebarGroup className="pb-1 pt-1">
      <SidebarGroupLabel>News</SidebarGroupLabel>
      <SidebarMenu
        className="gap-0.5"
        role={nested ? undefined : "tablist"}
        aria-label="Publication Source"
      >
        {visibleFeeds?.has("wire") !== false ? (
          <SidebarMenuItem>
            <SidebarMenuButton
              type="button"
              role={nested ? undefined : "tab"}
              aria-label="The Wire, Beta"
              aria-selected={nested ? undefined : wireActive}
              aria-current={nested && wireActive ? "page" : undefined}
              isActive={wireActive}
              onClick={onWireSelect}
            >
              <Rss />
              <span>The Wire</span>
              <WireBetaBadge className="ml-auto" />
            </SidebarMenuButton>
          </SidebarMenuItem>
        ) : null}
        {visibleFeeds?.has("circle") !== false ? (
          <SidebarMenuItem>
            <SidebarMenuButton
              type="button"
              role={nested ? undefined : "tab"}
              aria-label="Your Circle, Beta"
              className="gap-1.5"
              aria-selected={nested ? undefined : circleActive}
              aria-current={nested && circleActive ? "page" : undefined}
              isActive={circleActive}
              onClick={onCircleSelect}
            >
              <Network />
              <span className="whitespace-nowrap">Your&nbsp;Circle</span>
              <WireBetaBadge className="ml-auto" />
            </SidebarMenuButton>
          </SidebarMenuItem>
        ) : null}
        {visibleFeeds?.has("subscribed") !== false ? (
          <SidebarMenuItem>
            <SidebarReadBulkMenuWrap
              publications={subscribedPublications}
              gatewayScopes={[{ kind: "subscribed" }]}
              markAllReadConfirmation="This marks every unread article in Subscribed as read."
              showMarkAllUnread={false}
            >
              <SidebarMenuButton
                type="button"
                role={nested ? undefined : "tab"}
                aria-selected={nested ? undefined : activeTab === "subscribed"}
                aria-current={nested && activeTab === "subscribed" ? "page" : undefined}
                isActive={activeTab === "subscribed"}
                onClick={() => onTabChange("subscribed")}
              >
                <Newspaper />
                <span>Subscribed</span>
                {showSubscribedUnreadCount && subscribedUnread > 0 ? (
                  <SidebarSectionUnreadBadge count={subscribedUnread} />
                ) : null}
              </SidebarMenuButton>
            </SidebarReadBulkMenuWrap>
            {subscribedChildren ? (
              <SidebarMenuSub className="pl-4" aria-label="Subscribed Sources">
                {subscribedChildren}
              </SidebarMenuSub>
            ) : null}
          </SidebarMenuItem>
        ) : null}
        {visibleFeeds?.has("following") !== false ? (
          <SidebarMenuItem>
            <SidebarReadBulkMenuWrap
              publications={followingPublications}
              gatewayScopes={[{ kind: "following" }]}
              markAllReadConfirmation="This marks every unread article in Following as read."
              showMarkAllUnread={false}
            >
              <SidebarMenuButton
                type="button"
                role={nested ? undefined : "tab"}
                aria-selected={nested ? undefined : activeTab === "following"}
                aria-current={nested && activeTab === "following" ? "page" : undefined}
                isActive={activeTab === "following"}
                onClick={() => onTabChange("following")}
              >
                <Users />
                <span>Following</span>
                {showFollowingUnreadCount && followingUnread > 0 ? (
                  <SidebarSectionUnreadBadge count={followingUnread} />
                ) : null}
              </SidebarMenuButton>
            </SidebarReadBulkMenuWrap>
            {followingChildren ? (
              <SidebarMenuSub className="pl-4" aria-label="Following Sources">
                {followingChildren}
              </SidebarMenuSub>
            ) : null}
          </SidebarMenuItem>
        ) : null}
      </SidebarMenu>
    </SidebarGroup>
  );
}
