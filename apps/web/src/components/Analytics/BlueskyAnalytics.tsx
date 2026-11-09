import Link from "next/link";
import type { BlueskyAnalyticsSummary } from "@/lib/analytics/blueskyAnalyticsClient";
import { metricLabel, safeAnalyticsUrl } from "@/lib/analytics/creatorAnalyticsClient";
import { Button } from "@/components/ui/button";
import { SocialPermissionRecovery } from "@/components/Social/SocialPermissionRecovery";
import { BlueskyPostTable } from "./BlueskyPostTable";

export function BlueskyAnalytics({ data, isPending, error, retry }: { data?: BlueskyAnalyticsSummary; isPending: boolean; error: unknown; retry: () => void }) {
  const message = error instanceof Error ? error.message : error ? "Bluesky analytics could not be loaded. Please try again." : undefined;
  const metrics = data ? ([
    ["Followers", data.profile.followers], ["Following", data.profile.following], ["Total Posts", data.profile.posts], ["Posts in Period", data.activity?.posts],
  ] as const).filter(([, value]) => value != null && Number.isFinite(value)) : [];
  return <section aria-label="Bluesky Analytics" className="space-y-5">
    <div><h2 className="font-semibold">Bluesky Overview</h2><p className="mt-1 text-xs text-muted-foreground">Public Bluesky Activity · {data ? `Updated ${new Date(data.sampledAt).toLocaleString()}` : "Current Counts"}</p></div>
    {isPending ? <p role="status" className="text-sm text-muted-foreground">Loading Bluesky Analytics…</p> : null}
    {message ? <div role="alert" className="rounded-xl border border-destructive/40 p-4 text-sm"><p>{message}</p>{/log in again/i.test(message) ? <SocialPermissionRecovery /> : <Button size="sm" variant="outline" className="mt-2" onClick={retry}>Retry Bluesky Analytics</Button>}</div> : null}
    {data ? <>
      {metrics.length ? <dl className="grid grid-cols-2 gap-3 lg:grid-cols-4">{metrics.map(([label, value]) => <div key={label} className="rounded-2xl border bg-card p-4"><dt className="text-xs text-muted-foreground">{label}</dt><dd className="mt-2 text-2xl font-semibold tabular-nums">{metricLabel(value)}</dd></div>)}</dl> : null}
      {data.activity ? <p className="text-xs text-muted-foreground">Last {data.days} Days · {metricLabel(data.activity.replies)} Replies · {data.activity.postsPerDay.toLocaleString(undefined, { maximumFractionDigits: 1 })} Posts per Day · Reposts Excluded{!data.coverage.feedComplete ? " · Latest Posts Only; Period May Be Incomplete" : ""}</p> : null}
      <section aria-label="Top Bluesky Posts" className="space-y-3"><h3 className="font-semibold">Top Posts</h3><p className="text-xs text-muted-foreground">Posts Published in the Last {data.days} Days · Current Engagement Counts Can Include Activity Outside This Period</p>
        {data.topPosts.length ? <BlueskyPostTable posts={data.topPosts} /> : <p className="text-sm text-muted-foreground">{data.coverage.feedAvailable ? "No Posts Found in This Period." : "Bluesky Posts Are Temporarily Unavailable."}</p>}
      </section>
      <section aria-label="Discovered Article Shares" className="space-y-3"><h3 className="font-semibold">Discovered Article Shares</h3><p className="text-xs text-muted-foreground">Bluesky Posts Sharing Your Article URLs in the Last {data.days} Days · Current Engagement Counts · Search May Miss Shares</p>
        {!data.coverage.sharesAvailable ? <p role="status" className="text-sm text-muted-foreground">Some Article Share Searches Are Temporarily Unavailable. Available Results Are Shown Below.</p> : null}
        {data.coverage.articlesSearched === 0 && data.coverage.sharesAvailable ? <p className="text-sm text-muted-foreground">Refresh to Collect Articles With Public URLs for Share Discovery.</p> : <p className="text-xs text-muted-foreground">{data.coverage.articlesSearched} Article URLs Searched · A Limited Selection of Your Latest Collected Articles</p>}
        {data.shares.map(article => {
          const href = safeAnalyticsUrl(article.articleUrl);
          return <div key={article.articleUri} className="space-y-2"><h4 className="break-words text-sm font-medium">{href ? <Link className="hover:underline" href={href}>{article.articleTitle || "Untitled Article"}</Link> : article.articleTitle || "Untitled Article"}</h4>{article.posts.length ? <BlueskyPostTable posts={article.posts} /> : <p className="text-sm text-muted-foreground">No Shares Discovered for This Article.</p>}</div>;
        })}
        {data.coverage.articlesSearched > 0 && !data.shares.length ? <p className="text-sm text-muted-foreground">{data.coverage.sharesAvailable ? "No Article Shares Discovered in This Period." : "No Shares Discovered in Available Results."}</p> : null}
      </section>
    </> : null}
  </section>;
}
