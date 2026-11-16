"use client";

import { memo, useLayoutEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSkejScheduling } from "@/hooks/useSkejScheduling";
import { getAtprotoNetwork } from "@/lib/atprotoNetwork";
import { getBrandAccountPublicProfile } from "@/lib/brandAccounts/brandAccountPublicProfile";
import type { SkejScheduleSummary } from "@/lib/skej/skejSchedulingClient";

export type CalendarAccount = ReturnType<typeof useSkejScheduling> & {
  label: string;
  items: SkejScheduleSummary[];
};

export const ContentCalendarAccount = memo(function ContentCalendarAccount({ ownerDid, actorDid, fallbackLabel, showIdentity, report }: {
  ownerDid: string;
  actorDid: string;
  fallbackLabel: string;
  showIdentity: boolean;
  report: (did: string, account?: CalendarAccount) => void;
}) {
  const scheduler = useSkejScheduling(ownerDid, actorDid);
  const appView = getAtprotoNetwork().publicAppView;
  const profile = useQuery({
    queryKey: ["brandPublicProfile", actorDid, appView],
    queryFn: ({ signal }) => getBrandAccountPublicProfile(actorDid, appView, signal),
    enabled: showIdentity,
    staleTime: 300_000,
    retry: false,
  });
  const label = profile.data?.did === actorDid ? `@${profile.data.handle}` : fallbackLabel;
  const items = scheduler.readiness.data?.connected
    ? (scheduler.schedules.data ?? []).filter(item => item.did === actorDid)
    : [];

  useLayoutEffect(() => {
    report(actorDid, { ...scheduler, label, items });
  });
  useLayoutEffect(() => () => report(actorDid), [actorDid, report]);

  return null;
});
