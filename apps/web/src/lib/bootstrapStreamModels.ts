import type { EntryListItem } from "@/lib/atprotoClient";
import type { PublicationSidebarProjection } from "@/lib/publicationProjectionClient";

export type BootstrapStreamEventKind =
  | "sidebarPriority"
  | "unreadCounts"
  | "selectedPublication"
  | "entriesPage"
  | "sidebarFolders"
  | "warning"
  | "error"
  | "done";

export type BootstrapStreamEvent = {
  kind: BootstrapStreamEventKind;
  sidebarPriority?: PublicationSidebarProjection;
  unreadCounts?: { counts: Record<string, number> };
  selectedPublication?: { publicationId: string };
  entriesPage?: {
    publicationId: string;
    entries: EntryListItem[];
    cursor?: string;
  };
  sidebarFolders?: {
    folderSections: NonNullable<PublicationSidebarProjection["folderSections"]>;
    allPublicationRows: PublicationSidebarProjection["allPublicationRows"];
  };
  warning?: { message: string };
  error?: { message: string };
  done?: { refreshedAt: string };
};

export type ParsedBootstrapStreamEvent =
  | { kind: "sidebarPriority"; payload: PublicationSidebarProjection }
  | { kind: "unreadCounts"; payload: { counts: Record<string, number> } }
  | { kind: "selectedPublication"; payload: { publicationId: string } }
  | {
      kind: "entriesPage";
      payload: {
        publicationId: string;
        entries: EntryListItem[];
        cursor?: string;
      };
    }
  | {
      kind: "sidebarFolders";
      payload: {
        folderSections: NonNullable<PublicationSidebarProjection["folderSections"]>;
        allPublicationRows: PublicationSidebarProjection["allPublicationRows"];
      };
    }
  | { kind: "warning"; payload: { message: string } }
  | { kind: "error"; payload: { message: string } }
  | { kind: "done"; payload: { refreshedAt: string } };

export function parseBootstrapStreamEvent(
  raw: BootstrapStreamEvent
): ParsedBootstrapStreamEvent | null {
  switch (raw.kind) {
    case "sidebarPriority":
      if (!raw.sidebarPriority) return null;
      return { kind: "sidebarPriority", payload: raw.sidebarPriority };
    case "unreadCounts":
      if (!raw.unreadCounts) return null;
      return { kind: "unreadCounts", payload: raw.unreadCounts };
    case "selectedPublication":
      if (!raw.selectedPublication) return null;
      return { kind: "selectedPublication", payload: raw.selectedPublication };
    case "entriesPage":
      if (!raw.entriesPage) return null;
      return { kind: "entriesPage", payload: raw.entriesPage };
    case "sidebarFolders":
      if (!raw.sidebarFolders) return null;
      return { kind: "sidebarFolders", payload: raw.sidebarFolders };
    case "warning":
      if (!raw.warning) return null;
      return { kind: "warning", payload: raw.warning };
    case "error":
      if (!raw.error) return null;
      return { kind: "error", payload: raw.error };
    case "done":
      if (!raw.done) return null;
      return { kind: "done", payload: raw.done };
    default:
      return null;
  }
}

export function firstUnreadPriorityPublicationId(args: {
  myPublications: PublicationSidebarProjection["myPublications"];
  subscribedUnfoldered: PublicationSidebarProjection["subscribedUnfoldered"];
  followingTabPublications: PublicationSidebarProjection["followingTabPublications"];
  unreadCounts: Record<string, number>;
}): string | null {
  for (const row of [
    ...args.myPublications,
    ...args.subscribedUnfoldered,
    ...args.followingTabPublications,
  ]) {
    const count = args.unreadCounts[row.publicationId] ?? row.unreadCount ?? 0;
    if (count > 0) return row.publicationId;
  }
  return null;
}
