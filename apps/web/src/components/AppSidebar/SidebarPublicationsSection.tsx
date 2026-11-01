"use client";

import { memo } from "react";

import { AddPublicationDialog } from "./AddPublicationDialog";
import { CollapsibleSidebarSubSection } from "./CollapsibleSidebarSubSection";
import { PublicationMenuSubEntries } from "./PublicationMenuSubEntries";
import type { PublicationSidebarTab } from "./PublicationSubItem";
import type { DiscoveredPublication } from "@/lib/atprotoClient";
import type {
  FolderRecord,
  PublicationPrefsRecord,
  RepoRecord,
} from "@/lib/pdsClient";
import type { GatewayMarkAllReadScope } from "@/lib/publicationProjectionClient";
import { SidebarMenuSubItem } from "@/components/ui/sidebar";

export type SidebarPublicationsSectionProps = {
  inline?: boolean;
  publications: DiscoveredPublication[];
  publicationUnreadCounts: Map<string, number>;
  selectedPubId: string | null;
  onSelectPub: (pubId: string) => void;
  folders: RepoRecord<FolderRecord>[];
  prefsMap: Map<string, RepoRecord<PublicationPrefsRecord>>;
  sidebarTab: PublicationSidebarTab;
  listLoading: boolean;
  readBulkMarkAllReadConfirmation: React.ReactNode;
  gatewayMarkAllReadScopes?: GatewayMarkAllReadScope[];
};

function SidebarPublicationsSectionInner({
  inline = false,
  publications,
  publicationUnreadCounts,
  selectedPubId,
  onSelectPub,
  folders,
  prefsMap,
  sidebarTab,
  listLoading,
  readBulkMarkAllReadConfirmation,
  gatewayMarkAllReadScopes,
}: SidebarPublicationsSectionProps) {
  const subAriaLabel =
    sidebarTab === "subscribed"
      ? "Subscribed Publications"
      : "Publications From Followed Accounts";

  const entries = (
    <>
      <PublicationMenuSubEntries
        publications={publications}
        publicationUnreadCounts={publicationUnreadCounts}
        selectedPubId={selectedPubId}
        onSelectPub={onSelectPub}
        folders={folders}
        prefsMap={prefsMap}
        sidebarTab={sidebarTab}
        listLoading={listLoading}
      />
      {!listLoading ? (
        <SidebarMenuSubItem className="p-0">
          <AddPublicationDialog />
        </SidebarMenuSubItem>
      ) : null}
    </>
  );
  if (inline) return entries;

  return (
    <CollapsibleSidebarSubSection
      title="Publications"
      unreadCount={0}
      subAriaLabel={subAriaLabel}
      readBulkPublications={publications}
      readBulkMarkAllReadConfirmation={readBulkMarkAllReadConfirmation}
      gatewayMarkAllReadScopes={gatewayMarkAllReadScopes}
    >
      {entries}
    </CollapsibleSidebarSubSection>
  );
}

export const SidebarPublicationsSection = memo(SidebarPublicationsSectionInner);
