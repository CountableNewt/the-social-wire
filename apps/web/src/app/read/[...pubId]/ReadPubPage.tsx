"use client";

import dynamic from "next/dynamic";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { EntryList } from "@/components/EntryList/EntryList";
import { RssArticleReaderDialog } from "@/components/EntryDetail/RssArticleReaderDialog";
import { DevRecordKindBadge } from "@/components/shared/DevRecordKindBadge";
import { useReadRoute } from "@/contexts/ReadRouteContext";
import { recordKindFromPubId } from "@/lib/recordKindDebug";
import { entryOpenTarget, type EntryOpenTarget } from "@/lib/entryOpenTarget";
import { resolveEntryOpenUrlFromPds } from "@/lib/resolveEntryOpenUrl";
import type { EntryListItem } from "@/lib/atprotoClient";
import type { AggregateAppViewFeed } from "@/lib/thinAppViewClient";
import { useFeedDisplayPreferences } from "@/hooks/useFeedDisplayPreferences";
import { useAuth } from "@/hooks/useAuth";
import { useDeferredArticleRead } from "@/hooks/useDeferredArticleRead";
import { isWireNewsEditionEnabled } from "@/lib/wireEditionClient";
import { cn } from "@/lib/utils";

const ignoreTheWireReadState = () => undefined;
const theWireEntryIsUnread = () => false;

const WireNewsExperience = dynamic(
  () => import("@/components/Wire/WireNewsExperience"),
  {
    loading: () => (
      <div className="grid h-full gap-4 p-4 sm:grid-cols-2">
        <div className="h-80 animate-pulse rounded-2xl bg-muted" />
        <div className="h-80 animate-pulse rounded-2xl bg-muted" />
      </div>
    ),
  },
);

const CircleNewsExperience = dynamic(
  () => import("@/components/Circle/CircleNewsExperience"),
  {
    loading: () => (
      <div className="grid h-full gap-4 p-4 sm:grid-cols-2">
        <div className="h-80 animate-pulse rounded-2xl bg-muted" />
        <div className="h-80 animate-pulse rounded-2xl bg-muted" />
      </div>
    ),
  },
);

export default function ReadPubPage({
  pubId,
  aggregateFeed,
  wireFeed = false,
  circleFeed = false,
}: {
  pubId?: string;
  aggregateFeed?: AggregateAppViewFeed;
  wireFeed?: boolean;
  circleFeed?: boolean;
}) {
  const wireNewsEditionEnabled = wireFeed && isWireNewsEditionEnabled();
  const editorialFeed = wireFeed || circleFeed;
  const [rssReaderSelection, setRssReaderEntry] = useState<{
    entryId: string;
    title: string;
    url: string;
    viewerKey: string;
    scope: string;
  } | null>(null);
  const [resolvingSelection, setResolvingSelection] = useState<{ entryId: string; viewerKey: string; scope: string } | null>(null);
  const [openFailureMessage, setOpenFailureMessage] = useState<string | null>(
    null,
  );
  const publicationKind = pubId ? recordKindFromPubId(pubId) : null;
  const { preferences } = useFeedDisplayPreferences();
  const { session, getOAuthSession, oauthSessionReloadSeq } = useAuth();
  const viewerKey = `${session?.did ?? "public"}:${oauthSessionReloadSeq ?? 0}`;
  const scope = JSON.stringify([pubId, aggregateFeed, wireFeed, circleFeed]);
  const rssReaderEntry = rssReaderSelection?.viewerKey === viewerKey && rssReaderSelection.scope === scope ? rssReaderSelection : null;
  const resolvingEntryId = resolvingSelection?.viewerKey === viewerKey && resolvingSelection.scope === scope ? resolvingSelection.entryId : null;
  const setResolvingEntryId = useCallback((entryId: string | null) => setResolvingSelection(entryId ? { entryId, viewerKey, scope } : null), [scope, viewerKey]);
  const openContext = useRef({ viewerKey, scope, intent: 0, mounted: true });
  useLayoutEffect(() => { openContext.current = { viewerKey, scope, intent: openContext.current.intent + 1, mounted: true }; }, [viewerKey, scope]);
  useEffect(() => {
    openContext.current.mounted = true;
    return () => { openContext.current.mounted = false; openContext.current.intent++; };
  }, []);
  const {
    markEntryRead,
    markEntryUnread,
    isEntryRead,
    pendingEntryReadState,
    articleListFilter,
  } = useReadRoute();

  const markOptions = useMemo(
    () => (pubId ? { publicationId: pubId } : undefined),
    [pubId],
  );

  const markEntryReadForPub = useCallback(
    (entryId: string) => markEntryRead(entryId, markOptions),
    [markEntryRead, markOptions]
  );
  const { open: deferRead, finish: finishReading, cancel: cancelDeferredRead } = useDeferredArticleRead({ scope, viewerKey, markRead: markEntryReadForPub });
  const deferEntryRead = useCallback((entryId: string) => {
    const oauth = getOAuthSession();
    deferRead(entryId, () => getOAuthSession() === oauth);
  }, [deferRead, getOAuthSession]);
  const markEntryReadExplicitly = useCallback((entryId: string) => {
    cancelDeferredRead(entryId);
    markEntryReadForPub(entryId);
  }, [cancelDeferredRead, markEntryReadForPub]);

  const markEntryUnreadForPub = useCallback(
    (entryId: string) => { cancelDeferredRead(entryId); markEntryUnread(entryId, markOptions); },
    [cancelDeferredRead, markEntryUnread, markOptions]
  );

  const openEntryTarget = useCallback(
    (
      entryId: string,
      entry: EntryListItem,
      target: EntryOpenTarget,
      pendingTab?: Window | null,
    ) => {
      if (target.kind === "rssReader") {
        pendingTab?.close();
        if (!editorialFeed) deferEntryRead(entryId);
        setRssReaderEntry({
          entryId,
          title: entry.title ?? "RSS Article",
          url: target.url,
          viewerKey,
          scope,
        });
        return;
      }

      const tab = pendingTab === undefined ? window.open("", "_blank") : pendingTab;
      if (!tab || tab.closed) { setOpenFailureMessage("Allow Pop-Ups to Open This Article."); return; }
      tab.opener = null;
      tab.location.href = target.url;
      if (!editorialFeed) deferEntryRead(entryId);
    },
    [deferEntryRead, editorialFeed, scope, viewerKey],
  );

  const handleSelectEntry = useCallback(
    (entryId: string, entry?: EntryListItem) => {
      if (!entry) return;
      const context = openContext.current;
      const intent = ++context.intent;
      const oauth = getOAuthSession();
      setResolvingEntryId(null);
      setOpenFailureMessage(null);
      const target = entryOpenTarget(entry, preferences.rssArticleOpenMode);
      if (target) {
        openEntryTarget(entryId, entry, target);
        return;
      }

      // The AppView has no indexed URL for this entry, so fall back to the author's PDS. Claim
      // the tab synchronously — after an `await` the user gesture is gone and Safari blocks it.
      // `noopener`/`noreferrer` in the features string make window.open return null, which would
      // lose the handle we need to navigate it, so sever the opener by hand instead. The tab is
      // still about:blank here, so it inherits our origin and the assignment sticks.
      const pendingTab = window.open("", "_blank");
      if (pendingTab) pendingTab.opener = null;
      setResolvingEntryId(entryId);
      void (async () => {
        let resolvedUrl: string | undefined;
        try {
          resolvedUrl = await resolveEntryOpenUrlFromPds(
            entryId,
            oauth ?? undefined,
          );
        } finally {
          if (openContext.current === context && context.mounted && context.intent === intent) setResolvingEntryId(null);
        }

        if (openContext.current !== context || !context.mounted || context.intent !== intent || getOAuthSession() !== oauth) { pendingTab?.close(); return; }

        const resolvedTarget = resolvedUrl
          ? entryOpenTarget(
              { ...entry, originalUrl: resolvedUrl },
              preferences.rssArticleOpenMode,
            )
          : null;
        if (!resolvedTarget) {
          pendingTab?.close();
          setOpenFailureMessage("Couldn't Find A Link For This Article.");
          return;
        }
        openEntryTarget(entryId, entry, resolvedTarget, pendingTab);
      })();
    },
    [
      getOAuthSession,
      openEntryTarget,
      preferences.rssArticleOpenMode,
      setResolvingEntryId,
    ],
  );

  const closeRssReader = useCallback(() => {
    if (!editorialFeed) finishReading();
    setRssReaderEntry(null);
  }, [editorialFeed, finishReading]);

  return (
    <>
      <div className="mx-auto flex h-full min-h-0 w-full flex-1 flex-col overflow-hidden bg-background">
        {publicationKind ? (
          <div className="shrink-0 px-3 pt-2">
            <DevRecordKindBadge info={publicationKind} />
          </div>
        ) : null}
        {openFailureMessage ? (
          <div className="shrink-0 px-3 pt-2">
            <div
              role="status"
              aria-live="polite"
              className="flex items-center justify-between gap-3 rounded-xl border border-border bg-muted/35 px-4 py-3 text-sm text-muted-foreground"
            >
              <span>{openFailureMessage}</span>
              <button
                type="button"
                className="shrink-0 text-primary underline-offset-4 hover:underline"
                onClick={() => setOpenFailureMessage(null)}
              >
                Dismiss
              </button>
            </div>
          </div>
        ) : null}
        <div
          className={cn(
            "min-h-0 flex-1 overflow-hidden",
            (aggregateFeed?.kind === "subscribed" ||
              aggregateFeed?.kind === "following") &&
              "mx-auto w-full max-w-3xl",
          )}
        >
          {circleFeed ? (
            <CircleNewsExperience onSelect={handleSelectEntry} />
          ) : wireNewsEditionEnabled ? (
            <WireNewsExperience onSelect={handleSelectEntry} />
          ) : (
            <EntryList
              pubId={pubId}
              aggregateFeed={aggregateFeed}
              wireFeed={wireFeed}
              selectedEntryId={rssReaderEntry?.entryId ?? null}
              resolvingEntryId={resolvingEntryId}
              onSelectEntry={handleSelectEntry}
              isEntryRead={editorialFeed ? theWireEntryIsUnread : isEntryRead}
              pendingEntryReadState={editorialFeed ? undefined : pendingEntryReadState}
              readIndicatorsEnabled={!editorialFeed}
              articleFilter={editorialFeed ? "all" : articleListFilter}
              markEntryRead={
                editorialFeed ? ignoreTheWireReadState : markEntryReadExplicitly
              }
              markEntryUnread={
                editorialFeed ? ignoreTheWireReadState : markEntryUnreadForPub
              }
            />
          )}
        </div>
      </div>
      <RssArticleReaderDialog
        open={rssReaderEntry !== null}
        entryId={rssReaderEntry?.entryId ?? null}
        originalUrl={rssReaderEntry?.url ?? null}
        title={rssReaderEntry?.title ?? ""}
        onClose={closeRssReader}
      />
    </>
  );
}
