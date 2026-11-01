"use client";

import { useQuery } from "@tanstack/react-query";
import { useAuth } from "@/hooks/useAuth";
import { useFinanceCatalog } from "@/hooks/useFinanceCatalog";
import { useSportsCatalog } from "@/hooks/useSportsCatalog";
import { useSportsDisplayLocale } from "@/hooks/useSportsDisplayLocale";
import { listFinanceSelections, type FinanceFeedDefinition, type FinanceSelection } from "@/lib/financeFeedClient";
import { listSportsSelections, type SportsEntity, type SportsFeedDefinition, type SportsSelection } from "@/lib/sportsFeedClient";
import { displaySportsEntityName } from "@/lib/sportsDisplayNames";
import { isDummyReaderDataEnabled } from "@/lib/dummyReaderData";

export function financeSidebarInterests(definitions: readonly FinanceFeedDefinition[], selections: readonly FinanceSelection[]) {
  const instruments = new Set(selections.filter(row => row.kind === "instrument").map(row => row.reference));
  const sectors = new Set(selections.filter(row => row.kind === "sector").map(row => row.reference));
  return definitions.filter(feed =>
    (feed.kind === "instrument" && feed.instrumentIDs.length === 1 && instruments.has(feed.instrumentIDs[0])) ||
    (feed.kind === "industry" && feed.sectorIDs.length === 1 && sectors.has(feed.sectorIDs[0])),
  ).map(feed => ({ id: feed.id, title: feed.kind === "instrument" ? feed.title.split(" · ", 2).join(" · ") : feed.title }))
    .sort((a, b) => a.title.localeCompare(b.title));
}

export function sportsSidebarInterests(entities: readonly SportsEntity[], definitions: readonly SportsFeedDefinition[], selections: readonly SportsSelection[], locale: string) {
  const muted = new Set(selections.filter(row => row.action === "mute").map(row => row.reference));
  const followed = new Set(selections.filter(row => row.action === "follow" && !muted.has(row.reference)).map(row => row.reference));
  const feeds = new Map(definitions.filter(feed => feed.kind !== "global" && feed.entityIDs.length === 1).map(feed => [feed.entityIDs[0], feed]));
  return entities.filter(entity => entity.active && followed.has(entity.id) && feeds.has(entity.id))
    .map(entity => ({ id: feeds.get(entity.id)!.id, title: displaySportsEntityName(entity, locale) }))
    .sort((a, b) => a.title.localeCompare(b.title));
}

/** Subscribe to the same interest caches as the readers without fetching timelines. */
export function useTopicSidebarInterests(topic: "finance" | "sports") {
  const { session, getOAuthSession, oauthSessionReloadSeq } = useAuth();
  const oauth = getOAuthSession();
  const dummy = isDummyReaderDataEnabled();
  const enabled = !!session && !!oauth && !dummy;
  const finance = useFinanceCatalog({ enabled: topic === "finance" });
  const sports = useSportsCatalog({ enabled: topic === "sports" });
  const financeSelections = useQuery({ queryKey: ["financeSelections", session?.did ?? "public", oauthSessionReloadSeq], queryFn: () => listFinanceSelections(oauth!, session!.did), enabled: enabled && topic === "finance", staleTime: 60000 });
  const sportsSelections = useQuery({ queryKey: ["sportsSelections", session?.did ?? "public", oauthSessionReloadSeq], queryFn: () => listSportsSelections(oauth!, session!.did), enabled: enabled && topic === "sports", staleTime: 60000 });
  const locale = useSportsDisplayLocale();
  const catalog = topic === "finance" ? finance : sports;
  const selections = topic === "finance" ? financeSelections : sportsSelections;
  const rows = topic === "finance"
    ? financeSidebarInterests(finance.data?.feeds ?? [], financeSelections.data ?? [])
    : sportsSidebarInterests(sports.data?.entities ?? [], sports.data?.feeds ?? [], sportsSelections.data ?? [], locale);
  return {
    rows,
    loading: catalog.isPending || (!dummy && !!session && (!oauth || (enabled && selections.isPending))),
    error: !!catalog.error || !!selections.error,
  };
}
