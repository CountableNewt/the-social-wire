"use client";

import { useLayoutEffect, useRef } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getStoredOAuthDid } from "@/lib/auth";
import { useAuth } from "@/hooks/useAuth";
import { getCreatorAnalytics } from "@/lib/analytics/creatorAnalyticsClient";
import { getBlueskyAnalytics } from "@/lib/analytics/blueskyAnalyticsClient";
import { getPublicationAnalytics } from "@/lib/analytics/publicationAnalyticsClient";
import { listBrandAccounts, restoreBrandAccount } from "@/lib/brandAccounts/brandAccountSessions";
import { brandAccountsEnabled } from "@/lib/brandAccountsEnabled";

export function useCreatorAnalytics(days: number, accountDid?: string) {
  const { session, getOAuthSession, oauthSessionReloadSeq } = useAuth();
  const owner = session?.did;
  const actor = accountDid ?? owner;
  const brandsAvailable = brandAccountsEnabled();
  const client = useQueryClient();
  const identity = useRef({ owner, actor });
  const mounted = useRef(true);
  const brandRestore = useRef<{
    owner: string;
    actor: string;
    reloadSeq: number;
    session: ReturnType<typeof restoreBrandAccount>;
  } | null>(null);
  useLayoutEffect(() => { identity.current = { owner, actor }; mounted.current = true; return () => { mounted.current = false; }; }, [owner, actor]);
  function assertIdentity(expectedOwner = owner, expectedActor = actor) {
    if (!mounted.current || !expectedOwner || !expectedActor || identity.current.owner !== expectedOwner || identity.current.actor !== expectedActor || getOAuthSession()?.did !== expectedOwner || getStoredOAuthDid() !== expectedOwner) throw new Error("Your account changed. Please reload Analytics.");
    if (expectedActor !== expectedOwner && !brandAccountsEnabled()) throw new Error("Brand Accounts Are Not Available.");
    if (expectedActor !== expectedOwner && !listBrandAccounts(expectedOwner).includes(expectedActor)) throw new Error("This brand account is no longer linked.");
  }
  function restoreAnalyticsBrand(expectedOwner: string, expectedActor: string) {
    const pending = brandRestore.current;
    if (pending?.owner === expectedOwner && pending.actor === expectedActor && pending.reloadSeq === oauthSessionReloadSeq) {
      return pending.session;
    }
    const session = restoreBrandAccount(expectedOwner, expectedActor).finally(() => {
      if (brandRestore.current?.session === session) brandRestore.current = null;
    });
    brandRestore.current = { owner: expectedOwner, actor: expectedActor, reloadSeq: oauthSessionReloadSeq, session };
    return session;
  }
  async function requireSession(expectedOwner = owner, expectedActor = actor) {
    if (!expectedOwner || !expectedActor || !getOAuthSession()) throw new Error("Log in again to allow Analytics to access your account.");
    assertIdentity(expectedOwner, expectedActor);
    const oauth = expectedActor === expectedOwner ? getOAuthSession()! : await restoreAnalyticsBrand(expectedOwner, expectedActor);
    assertIdentity(expectedOwner, expectedActor);
    if (oauth.did !== expectedActor) throw new Error("Your account changed. Please reload Analytics.");
    return oauth;
  }
  const accountKey = [owner ?? "", oauthSessionReloadSeq, actor, brandsAvailable] as const;
  const blueskyKey = ["blueskyAnalytics", ...accountKey] as const;
  const publicationKey = ["publicationAnalytics", ...accountKey] as const;
  const enabled = !!owner && !!actor && (actor === owner || brandsAvailable);
  const blueskyQuery = useQuery({
    queryKey: [...blueskyKey, days],
    enabled,
    queryFn: async ({ signal }) => {
      const result = await getBlueskyAnalytics(await requireSession(owner, actor), { days, signal });
      assertIdentity(owner, actor);
      return result;
    },
    retry: false,
    staleTime: 5 * 60_000,
  });
  const publicationQuery = useQuery({
    queryKey: publicationKey,
    enabled,
    queryFn: async ({ signal }) => {
      const result = await getPublicationAnalytics(await requireSession(owner, actor), { signal });
      assertIdentity(owner, actor);
      return result;
    },
    retry: false,
    staleTime: 5 * 60_000,
  });
  const refresh = useMutation({
    mutationFn: async ({ did, actingDid, period }: { did: string; actingDid: string; period: number }) => {
      await getCreatorAnalytics(await requireSession(did, actingDid), { days: period, refresh: true });
      assertIdentity(did, actingDid);
    },
    onSettled: async (_result, _error, args) => {
      if (mounted.current && identity.current.owner === args.did && identity.current.actor === args.actingDid) {
        await Promise.all([
          client.invalidateQueries({ queryKey: blueskyKey }),
          client.invalidateQueries({ queryKey: publicationKey, exact: true }),
        ]);
      }
    },
  });
  return { blueskyQuery, publicationQuery, refresh, collect: () => refresh.mutateAsync({ did: owner!, actingDid: actor!, period: days }) };
}
