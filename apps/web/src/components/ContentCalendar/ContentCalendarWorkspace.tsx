"use client";

import { useCallback, useState } from "react";
import { FeedHeader } from "@/components/FeedHeader/FeedHeader";
import { SchedulingReadiness } from "@/components/Social/SchedulingReadiness";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogTitle } from "@/components/ui/dialog";
import { useAuth } from "@/hooks/useAuth";
import { useBrandAccounts } from "@/hooks/useBrandAccounts";
import { brandAccountsEnabled } from "@/lib/brandAccountsEnabled";
import { isSkejArticleSchedule, type SkejScheduleSummary } from "@/lib/skej/skejSchedulingClient";
import { CalendarMonth } from "./CalendarMonth";
import { ContentCalendarAccount, type CalendarAccount } from "./ContentCalendarAccount";
import { RescheduleDialog } from "./RescheduleDialog";
import { localCalendarDate } from "./calendarDates";

export function ContentCalendarWorkspace() {
  const { session } = useAuth();
  return session ? <AccountCalendarWorkspace key={session.did} ownerDid={session.did} /> : null;
}

function AccountCalendarWorkspace({ ownerDid }: { ownerDid: string }) {
  const linked = useBrandAccounts(ownerDid);
  const accountIds = [...new Set([ownerDid, ...(brandAccountsEnabled() ? linked : [])])];
  const showIdentity = accountIds.length > 1;
  const [accounts, setAccounts] = useState<Record<string, CalendarAccount>>({});
  const [editing, setEditing] = useState<SkejScheduleSummary>();
  const [canceling, setCanceling] = useState<SkejScheduleSummary>();
  const report = useCallback((did: string, account?: CalendarAccount) => {
    if (!account) {
      setEditing(current => current?.did === did ? undefined : current);
      setCanceling(current => current?.did === did ? undefined : current);
    }
    setAccounts(previous => {
      if (account) return { ...previous, [did]: account };
      const next = { ...previous };
      delete next[did];
      return next;
    });
  }, []);
  const [month, setMonth] = useState(() => new Date());
  const [day, setDay] = useState(() => localCalendarDate(new Date()));
  const [error, setError] = useState<string>();
  const sources = accountIds.flatMap(did => accounts[did] ? [accounts[did]] : []);
  const loading = sources.length < accountIds.length || sources.some(account =>
    !account.readiness.isError && (account.readiness.isPending || account.readiness.data?.connected && account.schedules.isPending)
  );
  const counts: Record<string, number> = {};
  const selected = sources.flatMap(account => account.items.flatMap(item => {
    const time = new Date(item.scheduledAt);
    if (!Number.isFinite(time.getTime())) return [];
    const date = localCalendarDate(time);
    counts[date] = (counts[date] ?? 0) + 1;
    return date === day ? [{ account, item, time }] : [];
  })).sort((a, b) => a.time.getTime() - b.time.getTime());
  const editingAccount = editing && accountIds.includes(editing.did) ? accounts[editing.did] : undefined;
  const cancelingAccount = canceling && accountIds.includes(canceling.did) ? accounts[canceling.did] : undefined;
  const cancelPending = !!cancelingAccount?.pending;

  async function cancel() {
    if (!canceling || !cancelingAccount || cancelPending) return;
    setError(undefined);
    try {
      await cancelingAccount.cancel(canceling);
      setCanceling(undefined);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Schedule Could Not Be Canceled.");
    }
  }

  function refresh() {
    for (const account of sources) {
      void account.readiness.refetch();
      if (account.readiness.data?.connected) void account.schedules.refetch();
    }
  }

  return <section data-analytics-ignore className="flex min-h-0 w-full flex-1 flex-col overflow-hidden">
    <FeedHeader title="Content Calendar">
      <Button size="sm" variant="outline" disabled={!sources.length || sources.some(account => account.schedules.isFetching || account.readiness.isFetching || account.pending)} onClick={refresh}>Refresh</Button>
    </FeedHeader>
    <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-4 pt-0">
      <div className="mx-auto w-full max-w-5xl space-y-4">
        {accountIds.map((did, index) => <ContentCalendarAccount key={did} ownerDid={ownerDid} actorDid={did}
          showIdentity={showIdentity} fallbackLabel={did === ownerDid ? "Personal Account" : `Delegated Account ${index}`} report={report} />)}
        {accountIds.map(did => {
          const account = accounts[did];
          if (!account || account.readiness.data?.connected && !account.readiness.isError && !account.schedules.isError) return null;
          return <div key={did} className="space-y-2">
            {showIdentity ? <p className="text-sm font-medium">{account.label}</p> : null}
            {(!account.readiness.data?.connected || account.readiness.isError) ? <SchedulingReadiness connected={account.readiness.data?.connected}
              pending={account.readiness.isPending} failed={account.readiness.isError} connectUrl={account.connectUrl}
              retry={() => { void account.readiness.refetch(); }} /> : null}
            {account.schedules.isError ? <p role="alert" className="rounded-xl border border-destructive/40 p-3 text-sm">Scheduled Posts Could Not Be Loaded. <button className="underline" onClick={() => { void account.schedules.refetch(); }}>Retry</button></p> : null}
          </div>;
        })}
        {loading ? <p role="status" className="text-sm text-muted-foreground">Loading Scheduled Posts…</p> : null}
        <CalendarMonth month={month} selectedDay={day} counts={counts} onMonthChange={setMonth} onSelectDay={setDay} />
        <section className="space-y-3">
          <h2 className="font-semibold">{new Date(`${day}T12:00:00`).toLocaleDateString(undefined, { weekday: "long", month: "long", day: "numeric", year: "numeric" })}</h2>
          {!selected.length && !loading && sources.some(account => account.readiness.data?.connected) && !sources.some(account => account.readiness.isError || account.schedules.isError)
            ? <p className="py-6 text-center text-sm text-muted-foreground">No Posts Scheduled for This Day</p> : null}
          {selected.map(({ account, item, time }) => <article key={`${item.did}:${item.scheduleUri}`} className="rounded-2xl border bg-card p-4">
            <header className="mb-2 flex flex-wrap items-center justify-between gap-2 text-sm">
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <time dateTime={item.scheduledAt}>{time.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" })}</time>
                {showIdentity ? <span className="break-all text-muted-foreground">{account.label}</span> : null}
              </div>
              <span className="rounded-full bg-muted px-2 py-0.5 capitalize">{item.status.replaceAll("_", " ")}</span>
            </header>
            {isSkejArticleSchedule(item) ? <p className="break-words text-sm font-medium">Blog Post · {item.record.shadowRecord.title}</p>
              : item.record.posts.map((post, index) => <p key={index} className="whitespace-pre-wrap break-words text-sm">{post.text}</p>)}
            {item.lastError?.message ? <p className="mt-2 text-sm text-destructive">{item.lastError.message}</p> : null}
            {["draft", "scheduled", "blocked", "failed"].includes(item.status) ? <div className="mt-3 flex gap-2">
              <Button size="sm" variant="outline" disabled={account.pending} onClick={() => setEditing(item)}>Reschedule</Button>
              <Button size="sm" variant="ghost" disabled={account.pending} onClick={() => { setError(undefined); setCanceling(item); }}>
                {isSkejArticleSchedule(item) ? "Cancel Scheduled Article" : "Cancel Scheduled Post"}
              </Button>
            </div> : null}
          </article>)}
        </section>
      </div>
    </div>
    {editing && editingAccount ? <RescheduleDialog key={editing.scheduleUri} initialTime={editing.scheduledAt}
      save={instant => editingAccount.reschedule(editing, instant)} close={() => setEditing(undefined)} /> : null}
    <Dialog open={!!cancelingAccount} onOpenChange={value => { if (!value && !cancelPending) setCanceling(undefined); }}>
      <DialogContent showCloseButton={!cancelPending}>
        <DialogTitle>{canceling && isSkejArticleSchedule(canceling) ? "Cancel Scheduled Article?" : "Cancel Scheduled Post?"}</DialogTitle>
        <DialogDescription>This Content Will Not Be Published by Skej. The Schedule Record Has Already Been Public.</DialogDescription>
        {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
        <DialogFooter>
          <DialogClose disabled={cancelPending} className="rounded-xl border px-3 py-2">Keep Scheduled</DialogClose>
          <Button variant="destructive" disabled={cancelPending} onClick={() => { void cancel(); }}>{cancelPending ? "Canceling…" : canceling && isSkejArticleSchedule(canceling) ? "Cancel Scheduled Article" : "Cancel Scheduled Post"}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </section>;
}
