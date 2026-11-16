"use client";

import type { LucideIcon } from "lucide-react";
import { Bookmark, FileText, Inbox, MessageCircle } from "lucide-react";
import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import type { SavedLinkCategory } from "@/lib/savedLinkCategory";

const CATEGORY_OPTIONS: ReadonlyArray<{
  category: SavedLinkCategory | null;
  label: string;
  icon: LucideIcon;
}> = [
  { category: null, label: "All Items", icon: Inbox },
  { category: "articles", label: "Articles", icon: FileText },
  { category: "social", label: "Social", icon: MessageCircle },
  { category: "other", label: "Other", icon: Bookmark },
];

export function SavedCategoryFilterSection({
  totalCount,
  categoryCounts,
  selectedCategory,
  onSelectCategory,
}: {
  totalCount: number;
  categoryCounts: Record<SavedLinkCategory, number>;
  selectedCategory: SavedLinkCategory | null;
  onSelectCategory: (category: SavedLinkCategory | null) => void;
}) {
  return (
    <SidebarGroup className="p-0 pb-3">
      <SidebarGroupLabel>Filter By</SidebarGroupLabel>
      <SidebarMenu aria-label="Filter Read Later By Type" className="gap-0.5">
        {CATEGORY_OPTIONS.map(({ category, label, icon: Icon }) => (
          <SidebarMenuItem key={label}>
            <SidebarMenuButton
              type="button"
              isActive={selectedCategory === category}
              onClick={() => onSelectCategory(category)}
              className="relative pr-8"
            >
              <Icon />
              <span>{label}</span>
              <SidebarMenuBadge>
                {category ? categoryCounts[category] : totalCount}
              </SidebarMenuBadge>
            </SidebarMenuButton>
          </SidebarMenuItem>
        ))}
      </SidebarMenu>
    </SidebarGroup>
  );
}
