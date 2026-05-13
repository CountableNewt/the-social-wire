"use client";

import { useCallback, useMemo, useState } from "react";
import { Check } from "lucide-react";
import { useWebHaptics } from "web-haptics/react";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { SidebarMenuSubButton, SidebarMenuSubItem } from "@/components/ui/sidebar";
import { Avatar } from "@/components/shared/Avatar";
import type { DiscoveredPublication } from "@/lib/atprotoClient";
import type { FolderRecord, PublicationPrefsRecord, RepoRecord } from "@/lib/pdsClient";
import { rkeyFromURI } from "@/lib/pdsClient";
import {
  useHidePublication,
  useSetPublicationFolder,
} from "@/hooks/usePublications";
import { ControlledCreateFolderDialog } from "./NewFolderDialog";

function notifyMutationFailure(label: string, err: unknown) {
  console.error(err);
  const message =
    err instanceof Error
      ? err.message
      : typeof err === "string"
        ? err
        : "Something went wrong. Try again.";
  window.alert(`${label}: ${message}`);
}

interface PublicationSubItemProps {
  publication: DiscoveredPublication;
  isSelected: boolean;
  onSelect: (publicationId: string) => void;
  folders: RepoRecord<FolderRecord>[];
  prefsMap: Map<string, RepoRecord<PublicationPrefsRecord>>;
}

export function PublicationSubItem({
  publication,
  isSelected,
  onSelect,
  folders,
  prefsMap,
}: PublicationSubItemProps) {
  const { trigger, isSupported } = useWebHaptics();
  const setFolder = useSetPublicationFolder();
  const hidePublication = useHidePublication();
  const [newFolderDialogOpen, setNewFolderDialogOpen] = useState(false);

  const prefs = prefsMap.get(publication.publicationId);
  const currentFolderId = prefs?.value.folderId ?? null;
  const isHidden = !!prefs?.value.hidden;

  const busy = setFolder.isPending || hidePublication.isPending;

  const hapticLight = useCallback(() => {
    if (isSupported) void trigger("light");
  }, [isSupported, trigger]);

  const hapticSuccess = useCallback(() => {
    if (isSupported) void trigger("success");
  }, [isSupported, trigger]);

  const handleOpenChange = useCallback(
    (open: boolean) => {
      if (open) hapticLight();
    },
    [hapticLight]
  );

  const assignFolder = useCallback(
    async (folderId: string | null) => {
      try {
        await setFolder.mutateAsync({
          publicationId: publication.publicationId,
          folderId,
          existingRkey: prefs ? rkeyFromURI(prefs.uri) : undefined,
        });
        hapticSuccess();
      } catch (e) {
        notifyMutationFailure("Could not move publication", e);
      }
    },
    [setFolder, publication.publicationId, prefs, hapticSuccess]
  );

  const setHidden = useCallback(
    async (hidden: boolean) => {
      try {
        await hidePublication.mutateAsync({
          publicationId: publication.publicationId,
          hidden,
          existingRkey: prefs ? rkeyFromURI(prefs.uri) : undefined,
        });
        hapticSuccess();
      } catch (e) {
        notifyMutationFailure(
          hidden ? "Could not hide publication" : "Could not unhide publication",
          e
        );
      }
    },
    [hidePublication, publication.publicationId, prefs, hapticSuccess]
  );

  const folderSubmenuLabel = useMemo(() => {
    if (!currentFolderId) return "Move to folder";
    const match = folders.find((f) => rkeyFromURI(f.uri) === currentFolderId);
    return match ? `In "${match.value.name}"` : "Move to folder";
  }, [currentFolderId, folders]);

  return (
    <SidebarMenuSubItem>
      <ContextMenu onOpenChange={handleOpenChange}>
        <ContextMenuTrigger className="flex min-w-0 w-full data-popup-open:bg-sidebar-accent">
          <SidebarMenuSubButton
            size="md"
            isActive={isSelected}
            render={<button type="button" />}
            onClick={() => onSelect(publication.publicationId)}
            className="min-w-0 flex-1 gap-2"
          >
            <PublicationLeadingAvatar publication={publication} />
            <span className="min-w-0 flex-1 truncate">{publication.title}</span>
          </SidebarMenuSubButton>
        </ContextMenuTrigger>
        <ContextMenuContent className="min-w-[11rem]">
          <ContextMenuItem
            disabled={busy}
            onClick={() => setNewFolderDialogOpen(true)}
          >
            New folder…
          </ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuSub>
            <ContextMenuSubTrigger disabled={busy}>{folderSubmenuLabel}</ContextMenuSubTrigger>
            <ContextMenuSubContent className="max-h-[min(50vh,280px)] overflow-y-auto">
              <ContextMenuItem
                disabled={busy}
                className="gap-2"
                onClick={() => void assignFolder(null)}
              >
                {currentFolderId === null ? (
                  <Check className="size-4 shrink-0 opacity-70" aria-hidden />
                ) : (
                  <span className="size-4 shrink-0" aria-hidden />
                )}
                <span className="truncate">All Publications</span>
              </ContextMenuItem>
              {folders.map((f) => {
                const rkey = rkeyFromURI(f.uri);
                const checked = currentFolderId === rkey;
                return (
                  <ContextMenuItem
                    key={f.uri}
                    disabled={busy}
                    className="gap-2"
                    onClick={() => void assignFolder(rkey)}
                  >
                    {checked ? (
                      <Check className="size-4 shrink-0 opacity-70" aria-hidden />
                    ) : (
                      <span className="size-4 shrink-0" aria-hidden />
                    )}
                    <span className="truncate">
                      {f.value.icon ? `${f.value.icon} ` : ""}
                      {f.value.name}
                    </span>
                  </ContextMenuItem>
                );
              })}
            </ContextMenuSubContent>
          </ContextMenuSub>
          <ContextMenuSeparator />
          {isHidden ? (
            <ContextMenuItem disabled={busy} onClick={() => void setHidden(false)}>
              Unhide publication
            </ContextMenuItem>
          ) : (
            <ContextMenuItem
              variant="destructive"
              disabled={busy}
              onClick={() => void setHidden(true)}
            >
              Hide publication
            </ContextMenuItem>
          )}
        </ContextMenuContent>
      </ContextMenu>
      <ControlledCreateFolderDialog
        open={newFolderDialogOpen}
        onOpenChange={setNewFolderDialogOpen}
        dialogTitle="New folder"
        description={`“${publication.title}” moves into this folder when you create it.`}
        submitLabel="Create & move"
        pendingSubmitLabel="Saving…"
        onCreated={async ({ uri }) => {
          await assignFolder(rkeyFromURI(uri));
        }}
      />
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
