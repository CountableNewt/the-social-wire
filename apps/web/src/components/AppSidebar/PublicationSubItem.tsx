"use client";

import { SidebarMenuSubButton, SidebarMenuSubItem } from "@/components/ui/sidebar";
import { Avatar } from "@/components/shared/Avatar";
import type { DiscoveredPublication } from "@/lib/atprotoClient";

interface PublicationSubItemProps {
  publication: DiscoveredPublication;
  isSelected: boolean;
  onSelect: (publicationId: string) => void;
}

export function PublicationSubItem({
  publication,
  isSelected,
  onSelect,
}: PublicationSubItemProps) {
  return (
    <SidebarMenuSubItem>
      <SidebarMenuSubButton
        size="md"
        isActive={isSelected}
        render={<button type="button" />}
        onClick={() => onSelect(publication.publicationId)}
        className="gap-2"
      >
        <PublicationLeadingAvatar publication={publication} />
        <span className="min-w-0 flex-1 truncate">{publication.title}</span>
      </SidebarMenuSubButton>
    </SidebarMenuSubItem>
  );
}

function PublicationLeadingAvatar({
  publication,
}: {
  publication: DiscoveredPublication;
}) {
  return (
    <Avatar
      src={publication.avatarUrl}
      alt=""
      size={20}
      className="shrink-0"
    />
  );
}
