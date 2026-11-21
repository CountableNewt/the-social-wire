"use client";

import { List, ListCollapse, RefreshCw } from "lucide-react";
import { usePathname, useSearchParams } from "next/navigation";

import { ReadListHeader } from "./ReadListHeader";
import { useReadRoute } from "@/contexts/ReadRouteContext";
import { useReadSidebarScope } from "@/contexts/ReadSidebarScopeContext";
import { useCachedBulkReadActions } from "@/hooks/useCachedBulkReadActions";
import { useIsTabletPortrait } from "@/hooks/use-tablet-portrait";
import { useClientHydrated } from "@/hooks/useClientHydrated";
import { useSidebarBootstrap } from "@/contexts/PublicationSidebarContext";
import { FeedHeader } from "@/components/FeedHeader/FeedHeader";
import { Button } from "@/components/ui/button";
import { FeedMarkReadButton } from "./FeedMarkReadButton";
import { ArticleReadFilter } from "@/components/FeedHeader/ArticleReadFilter";
import { cn } from "@/lib/utils";
import { PublicationSourceRefreshError } from "@/lib/publicationSourceRefreshError";
import { UnavailablePublicationActions } from "@/components/FeedHeader/UnavailablePublicationActions";

/**
 * Global All / Unread toggle for the read shell (applies to whichever publication is open).
 */
export function ReadArticleFilterBar() {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const listURI = pathname === "/read" ? searchParams.get("list") : null;

  const {
    setArticleListFilter,
    articleListFilter,
    isArticleListColumnOpen,
    setIsArticleListColumnOpen,
  } = useReadRoute();
  const isTabletPortrait = useIsTabletPortrait();
  const clientHydrated = useClientHydrated();
  const { refresh, refreshMessage, projectionError } = useSidebarBootstrap();

  const { activeFeedScope } = useReadSidebarScope();
  const { bulkDisabled, applyMarkAllRead, isMarkingRead, error: markReadError } =
    useCachedBulkReadActions(activeFeedScope.publications, {
      gatewayScopes: activeFeedScope.gatewayScope
        ? [activeFeedScope.gatewayScope]
        : [],
    });

  const markAllReadDisabled =
    bulkDisabled || isMarkingRead || !activeFeedScope.displayName.trim();
  const routeFeedTitle = pathname === "/read"
    ? searchParams.get("folder")
      ? "Folder"
      : searchParams.get("feed") === "following"
        ? "Following"
        : "Subscribed"
    : "Articles";
  const feedTitle = activeFeedScope.displayName.trim() || routeFeedTitle;

  if (listURI) return <ReadListHeader uri={listURI} />;

  const partialRefresh = refresh.isError && refresh.error instanceof PublicationSourceRefreshError ? refresh.error : undefined;
  const refreshStatus = refresh.isPending
    ? refreshMessage || "Refreshing Publications…"
    : partialRefresh ? partialRefresh.statusMessage : refresh.isError ? "Refresh Incomplete — Try Again" : refreshMessage;
  return (
    <FeedHeader title={feedTitle} subtitle={
      projectionError && !refreshMessage && !refresh.isError && !refresh.isPending ? <span role="alert" className="text-destructive" title={projectionError.message}>Couldn’t refresh. Try Refresh.</span> : refreshStatus ? <span role="status" aria-live="polite" aria-atomic="true" title={partialRefresh?.message} className={cn("hidden sm:inline", refresh.isError && !partialRefresh && "text-destructive")}>{refreshStatus}{partialRefresh ? <span className="sr-only">. {partialRefresh.message}</span> : null}</span> : undefined
    } mobileStatus={
      refreshStatus ? <span role="status" aria-live="polite" aria-atomic="true" className={refresh.isError && !partialRefresh ? "text-destructive" : undefined}>{refreshStatus}{partialRefresh ? <span className="block text-xs">{partialRefresh.hosts.join(", ")}</span> : null}</span> : undefined
    }>
      <div className="ml-auto flex min-w-0 max-w-full flex-wrap items-center justify-end gap-1">
        {partialRefresh ? <UnavailablePublicationActions key={refresh.submittedAt} warning={partialRefresh} /> : null}
        {isTabletPortrait ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="min-h-11 shrink-0 gap-1.5 rounded-md border-0 bg-transparent px-2 text-[11px] font-semibold text-muted-foreground shadow-none hover:bg-muted/50 hover:text-foreground"
            aria-label={
              isArticleListColumnOpen ? "Hide Articles" : "Show Articles"
            }
            aria-pressed={!isArticleListColumnOpen}
            onClick={() =>
              setIsArticleListColumnOpen(!isArticleListColumnOpen)
            }
          >
            {isArticleListColumnOpen ? (
              <ListCollapse aria-hidden="true" className="size-4" />
            ) : (
              <List aria-hidden="true" className="size-4" />
            )}
            {isArticleListColumnOpen ? "Hide Articles" : "Show Articles"}
          </Button>
        ) : null}
        <FeedMarkReadButton
          scope={activeFeedScope.gatewayScope}
          displayName={activeFeedScope.displayName}
          disabled={clientHydrated ? markAllReadDisabled : undefined}
          onMarkAllRead={applyMarkAllRead}
        />
        <ArticleReadFilter value={articleListFilter} onChange={setArticleListFilter} />
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          className="size-11 shrink-0 rounded-md border-0 bg-transparent shadow-none hover:bg-muted/50 hover:text-foreground sm:size-8"
          onClick={() => refresh.mutate()}
          disabled={refresh.isPending}
          aria-label={refresh.isPending ? "Refreshing Publications" : refresh.isError ? "Retry Refresh Publications" : "Refresh Publications"}
          aria-busy={refresh.isPending}
          title={refresh.isError ? refresh.error.message : refreshMessage || "Refresh Publications"}
        >
          <RefreshCw
            aria-hidden="true"
            className={`size-3.5 ${refresh.isPending ? "animate-spin" : ""}`}
          />
        </Button>
      </div>
      {markReadError ? <p role="alert" className="w-full text-xs text-destructive">{markReadError}</p> : null}
    </FeedHeader>
  );
}
