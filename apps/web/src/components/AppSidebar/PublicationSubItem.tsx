"use client";

import { memo, useCallback, useMemo, useState } from "react";
import {
  BookmarkPlus,
  BookmarkX,
  Check,
  FolderInput,
  FolderPlus,
  RefreshCw,
} from "lucide-react";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import {
  SidebarMenuBadge,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
} from "@/components/ui/sidebar";
import { Avatar } from "@/components/shared/Avatar";
import type { DiscoveredPublication } from "@/lib/atprotoClient";
import type { FolderRecord, PublicationPrefsRecord, RepoRecord } from "@/lib/pdsClient";
import { rkeyFromURI } from "@/lib/pdsClient";
import {
  useRefreshSkyreaderSubscriptionIcon,
  useSetPublicationFolder,
  useSubscribeToPublication,
  useUnsubscribePublication,
} from "@/hooks/usePublications";
import { useCachedBulkReadActions } from "@/hooks/useCachedBulkReadActions";
import { standardSiteSubscriptionTargetFromDiscovery } from "@/lib/publicationSubscriptionMatch";
import { isRssPublicationId } from "@/lib/rssFeedCore";
import { cn } from "@/lib/utils";
import { ControlledCreateFolderDialog } from "./NewFolderDialog";
import { FolderIconGlyph } from "./FolderIcon";

export type PublicationSidebarTab = "following" | "subscribed";

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
  /** Cache-only unread count from {@link useSidebarUnreadCounts}. */
  unreadCount: number;
  isSelected: boolean;
  onSelect: (publicationId: string) => void;
  folders: RepoRecord<FolderRecord>[];
  prefsMap: Map<string, RepoRecord<PublicationPrefsRecord>>;
  sidebarTab: PublicationSidebarTab;
}

function PublicationSubItemInner({
  publication,
  unreadCount,
  isSelected,
  onSelect,
  folders,
  prefsMap,
  sidebarTab,
}: PublicationSubItemProps) {
  const { trigger, isSupported } = useWebHaptics();
  const setFolder = useSetPublicationFolder();
  const subscribe = useSubscribeToPublication();
  const unsubscribe = useUnsubscribePublication();
  const refreshSkyreaderIcon = useRefreshSkyreaderSubscriptionIcon();
  const [newFolderDialogOpen, setNewFolderDialogOpen] = useState(false);
  const [unsubscribeDialogOpen, setUnsubscribeDialogOpen] = useState(false);
  const [markAllReadDialogOpen, setMarkAllReadDialogOpen] = useState(false);

  const bulkPublicationList = useMemo(() => [publication], [publication]);
  const {
    bulkDisabled,
    applyMarkAllRead,
    applyMarkAllUnread,
  } = useCachedBulkReadActions(bulkPublicationList, {
    gatewayScopes: [
      { kind: "publication", publicationId: publication.publicationId },
    ],
  });

  const prefs = prefsMap.get(publication.publicationId);
  const currentFolderId = prefs?.value.folderId ?? null;

  const subscribeTarget = useMemo(
    () => standardSiteSubscriptionTargetFromDiscovery(publication),
    [publication]
  );

  const canRefreshSkyreaderFavicon =
    Boolean(publication.subscriptionPublicationId) &&
    isRssPublicationId(publication.publicationId);

  const busy =
    setFolder.isPending ||
    subscribe.isPending ||
    unsubscribe.isPending ||
    refreshSkyreaderIcon.isPending;

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

  const handleSubscribe = useCallback(async () => {
    try {
      await subscribe.mutateAsync({ publication });
      hapticSuccess();
    } catch (e) {
      notifyMutationFailure("Could not subscribe", e);
    }
  }, [subscribe, publication, hapticSuccess]);

  const confirmUnsubscribe = useCallback(async () => {
    try {
      await unsubscribe.mutateAsync({ publication });
      setUnsubscribeDialogOpen(false);
      hapticSuccess();
    } catch (e) {
      notifyMutationFailure("Could not unsubscribe", e);
    }
  }, [unsubscribe, publication, hapticSuccess]);

  const handleRefreshSkyreaderFavicon = useCallback(async () => {
    try {
      await refreshSkyreaderIcon.mutateAsync({ publication });
      hapticSuccess();
    } catch (e) {
      notifyMutationFailure("Could not refresh favicon", e);
    }
  }, [refreshSkyreaderIcon, publication, hapticSuccess]);

  const folderSubmenuLabel = useMemo(() => {
    if (!currentFolderId) return "Move To Folder";
    const match = folders.find((f) => rkeyFromURI(f.uri) === currentFolderId);
    return match ? `In "${match.value.name}"` : "Move To Folder";
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
            className="relative min-w-0 flex-1 gap-2 pr-8"
          >
            <PublicationLeadingAvatar publication={publication} />
            <div className="flex min-w-0 flex-1 items-center">
              <span className="w-full truncate">{publication.title}</span>
            </div>
            <SidebarMenuBadge
              className={cn(
                "top-1/2 -translate-y-1/2",
                unreadCount <= 0 && "pointer-events-none opacity-0"
              )}
              aria-hidden={unreadCount <= 0}
              aria-label={
                unreadCount > 0 ? `${unreadCount} unread` : undefined
              }
            >
              {unreadCount > 0 ? unreadCount : 0}
            </SidebarMenuBadge>
          </SidebarMenuSubButton>
        </ContextMenuTrigger>
        <ContextMenuContent className="min-w-[11rem]">
          <ContextMenuItem
            disabled={busy}
            className="gap-2"
            onClick={() => setNewFolderDialogOpen(true)}
          >
            <FolderPlus className="size-4 shrink-0 opacity-70" aria-hidden />
            New Folder…
          </ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuSub>
            <ContextMenuSubTrigger disabled={busy} className="gap-2">
              <FolderInput className="size-4 shrink-0 opacity-70" aria-hidden />
              {folderSubmenuLabel}
            </ContextMenuSubTrigger>
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
                    <FolderIconGlyph
                      icon={f.value.icon}
                      iconImage={f.value.iconImage}
                      name={f.value.name}
                      className="size-4"
                    />
                    <span className="truncate">{f.value.name}</span>
                  </ContextMenuItem>
                );
              })}
            </ContextMenuSubContent>
          </ContextMenuSub>
          <ContextMenuSeparator />
          <ContextMenuItem
            disabled={busy || bulkDisabled}
            className="gap-2"
            onClick={() => setMarkAllReadDialogOpen(true)}
          >
            Mark All As Read
          </ContextMenuItem>
          <ContextMenuItem
            disabled={busy || bulkDisabled}
            className="gap-2"
            onClick={() => {
              applyMarkAllUnread();
              hapticSuccess();
            }}
          >
            Mark All As Unread
          </ContextMenuItem>
          {sidebarTab === "following" ? (
            <>
              <ContextMenuSeparator />
              <ContextMenuItem
                disabled={busy || subscribeTarget === null}
                className="gap-2"
                title={
                  subscribeTarget === null
                    ? "No Publication Record Or DID Available To Subscribe With."
                    : undefined
                }
                onClick={() => void handleSubscribe()}
              >
                <BookmarkPlus className="size-4 shrink-0 opacity-70" aria-hidden />
                {subscribe.isPending ? "Subscribing…" : "Subscribe"}
              </ContextMenuItem>
            </>
          ) : null}
          {sidebarTab === "subscribed" && canRefreshSkyreaderFavicon ? (
            <>
              <ContextMenuSeparator />
              <ContextMenuItem
                disabled={busy}
                className="gap-2"
                onClick={() => void handleRefreshSkyreaderFavicon()}
              >
                <RefreshCw className="size-4 shrink-0 opacity-70" aria-hidden />
                {refreshSkyreaderIcon.isPending ? "Refreshing…" : "Refresh Favicon"}
              </ContextMenuItem>
            </>
          ) : null}
          {sidebarTab === "subscribed" ? (
            <>
              <ContextMenuSeparator />
              <ContextMenuItem
                variant="destructive"
                disabled={busy}
                className="gap-2"
                onClick={() => setUnsubscribeDialogOpen(true)}
              >
                <BookmarkX className="size-4 shrink-0 opacity-70" aria-hidden />
                Unsubscribe
              </ContextMenuItem>
            </>
          ) : null}
        </ContextMenuContent>
      </ContextMenu>
      <ControlledCreateFolderDialog
        open={newFolderDialogOpen}
        onOpenChange={setNewFolderDialogOpen}
        dialogTitle="New Folder"
        description={`“${publication.title}” moves into this folder when you create it.`}
        submitLabel="Create & Move"
        pendingSubmitLabel="Saving…"
        onCreated={async ({ uri }) => {
          await assignFolder(rkeyFromURI(uri));
        }}
      />
      <Dialog open={unsubscribeDialogOpen} onOpenChange={setUnsubscribeDialogOpen}>
        <DialogContent showCloseButton>
          <DialogHeader>
            <DialogTitle>Unsubscribe?</DialogTitle>
            <DialogDescription>
              Stop receiving entries from “{publication.title}” in Subscribed. You can add this source
              again later from Add Publication.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={() => setUnsubscribeDialogOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={busy}
              onClick={() => void confirmUnsubscribe()}
            >
              {unsubscribe.isPending ? "Unsubscribing…" : "Unsubscribe"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={markAllReadDialogOpen} onOpenChange={setMarkAllReadDialogOpen}>
        <DialogContent showCloseButton>
          <DialogHeader>
            <DialogTitle>Mark All As Read?</DialogTitle>
            <DialogDescription>
              This marks every cached article in &quot;{publication.title}&quot; as read. Entries that
              have not been loaded yet stay unchanged until you open them.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={() => setMarkAllReadDialogOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              disabled={busy || bulkDisabled}
              onClick={() => {
                applyMarkAllRead();
                setMarkAllReadDialogOpen(false);
                hapticSuccess();
              }}
            >
              Mark All As Read
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </SidebarMenuSubItem>
  );
}

function folderSignature(folders: RepoRecord<FolderRecord>[]): string {
  return folders
    .map((folder) => `${rkeyFromURI(folder.uri)}:${folder.value.name ?? ""}`)
    .join("|");
}

function publicationSubItemPropsEqual(
  prev: PublicationSubItemProps,
  next: PublicationSubItemProps
): boolean {
  const prevPub = prev.publication;
  const nextPub = next.publication;
  return (
    prev.unreadCount === next.unreadCount &&
    prev.isSelected === next.isSelected &&
    prev.onSelect === next.onSelect &&
    prev.sidebarTab === next.sidebarTab &&
    prevPub.publicationId === nextPub.publicationId &&
    prevPub.subscriptionPublicationId === nextPub.subscriptionPublicationId &&
    prevPub.authorDid === nextPub.authorDid &&
    prevPub.authorHandle === nextPub.authorHandle &&
    prevPub.title === nextPub.title &&
    prevPub.iconUrl === nextPub.iconUrl &&
    prevPub.avatarUrl === nextPub.avatarUrl &&
    prev.prefsMap.get(prevPub.publicationId)?.value.folderId ===
      next.prefsMap.get(nextPub.publicationId)?.value.folderId &&
    folderSignature(prev.folders) === folderSignature(next.folders)
  );
}

export const PublicationSubItem = memo(
  PublicationSubItemInner,
  publicationSubItemPropsEqual
);

function PublicationLeadingAvatar({
  publication,
}: {
  publication: DiscoveredPublication;
}) {
  return (
    <Avatar
      src={publication.iconUrl ?? publication.avatarUrl}
      alt=""
      size={20}
      className="shrink-0"
    />
  );
}
