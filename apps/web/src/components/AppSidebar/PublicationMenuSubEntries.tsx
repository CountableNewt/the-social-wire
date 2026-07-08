"use client";

import { useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";

import type { DiscoveredPublication } from "@/lib/atprotoClient";
import type { FolderRecord, PublicationPrefsRecord, RepoRecord } from "@/lib/pdsClient";
import {
  PublicationSubItem,
  type PublicationSidebarTab,
} from "./PublicationSubItem";
import { SidebarSubMenuSkeletonRows } from "./SidebarSubMenuSkeletonRows";
import { SidebarMenuSubItem } from "@/components/ui/sidebar";

const VIRTUALIZED_PUBLICATION_COUNT = 80;
const SIDEBAR_PUBLICATION_ROW_HEIGHT = 32;

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
  const virtualScrollRef = useRef<HTMLDivElement>(null);
  const shouldVirtualize = publications.length >= VIRTUALIZED_PUBLICATION_COUNT;
  // eslint-disable-next-line react-hooks/incompatible-library -- TanStack Virtual owns scroll measurement internally.
  const virtualizer = useVirtualizer({
    count: shouldVirtualize ? publications.length : 0,
    getScrollElement: () => virtualScrollRef.current,
    estimateSize: () => SIDEBAR_PUBLICATION_ROW_HEIGHT,
    getItemKey: (index) => publications[index]?.publicationId ?? index,
    overscan: 8,
  });

  if (listLoading && publications.length === 0) {
    return <SidebarSubMenuSkeletonRows count={3} />;
  }

  if (publications.length === 0) {
    return null;
  }

  if (shouldVirtualize) {
    return (
      <SidebarMenuSubItem className="p-0">
        <div
          ref={virtualScrollRef}
          className="max-h-[min(60vh,520px)] overflow-y-auto overflow-x-hidden pr-1"
          data-testid="virtualized-publication-list"
        >
          <ul
            aria-label="Publications"
            className="relative min-w-0"
            style={{ height: virtualizer.getTotalSize() }}
          >
            {virtualizer.getVirtualItems().map((virtualRow) => {
              const pub = publications[virtualRow.index];
              if (!pub) return null;
              return (
                <PublicationSubItem
                  key={pub.publicationId}
                  publication={pub}
                  unreadCount={publicationUnreadCounts.get(pub.publicationId) ?? 0}
                  isSelected={selectedPubId === pub.publicationId}
                  onSelect={onSelectPub}
                  folders={folders}
                  prefsMap={prefsMap}
                  sidebarTab={sidebarTab}
                  className="absolute left-0 top-0 w-full"
                  style={{
                    height: SIDEBAR_PUBLICATION_ROW_HEIGHT,
                    transform: `translateY(${virtualRow.start}px)`,
                  }}
                />
              );
            })}
          </ul>
        </div>
      </SidebarMenuSubItem>
    );
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
