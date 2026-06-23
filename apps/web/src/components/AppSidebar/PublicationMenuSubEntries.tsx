"use client";

import type { DiscoveredPublication } from "@/lib/atprotoClient";
import type { FolderRecord, PublicationPrefsRecord, RepoRecord } from "@/lib/pdsClient";
import {
  PublicationSubItem,
  type PublicationSidebarTab,
} from "./PublicationSubItem";
import { SidebarSubMenuSkeletonRows } from "./SidebarSubMenuSkeletonRows";

export function PublicationMenuSubEntries({
  publications,
  publicationUnreadCounts,
  selectedPubId,
  onSelectPub,
  folders,
  prefsMap,
  sidebarTab,
  listLoading = false,
}: {
  publications: DiscoveredPublication[];
  publicationUnreadCounts: Map<string, number>;
  selectedPubId: string | null;
  onSelectPub: (pubId: string) => void;
  folders: RepoRecord<FolderRecord>[];
  prefsMap: Map<string, RepoRecord<PublicationPrefsRecord>>;
  sidebarTab: PublicationSidebarTab;
  listLoading?: boolean;
}) {
  if (listLoading && publications.length === 0) {
    return <SidebarSubMenuSkeletonRows count={3} />;
  }

  if (publications.length === 0) {
    return null;
  }

  return (
    <>
      {publications.map((pub) => (
        <PublicationSubItem
          key={pub.publicationId}
          publication={pub}
          unreadCount={publicationUnreadCounts.get(pub.publicationId) ?? 0}
          isSelected={selectedPubId === pub.publicationId}
          onSelect={onSelectPub}
          folders={folders}
          prefsMap={prefsMap}
          sidebarTab={sidebarTab}
        />
      ))}
    </>
  );
}
