import Link from "next/link";
import { metricLabel } from "@/lib/analytics/creatorAnalyticsClient";
import type { BlueskyAnalyticsPost } from "@/lib/analytics/blueskyAnalyticsClient";
import { isSocialPostUri, socialPostPageHref } from "@/lib/socialPostNavigation";

export function BlueskyPostTable({ posts }: { posts: BlueskyAnalyticsPost[] }) {
  const metrics = ([ ["likes", "Likes"], ["replies", "Replies"], ["reposts", "Reposts"], ["quotes", "Quotes"] ] as const)
    .filter(([key]) => posts.some(post => post.engagement?.[key] != null && Number.isFinite(post.engagement[key])));
  return <div className="overflow-x-auto rounded-xl border"><table className="w-full text-left text-xs">
    <thead className="bg-muted/40"><tr>{["Post", ...metrics.map(([, label]) => label)].map(label => <th key={label} scope="col" className="whitespace-nowrap px-3 py-3 font-medium">{label}</th>)}</tr></thead>
    <tbody>{posts.map(post => <tr key={post.uri} className="border-t">
      <td className="min-w-48 max-w-96 px-3 py-3"><p className="mb-1 text-muted-foreground">{post.publishedAt && Number.isFinite(Date.parse(post.publishedAt)) ? new Date(post.publishedAt).toLocaleDateString() : "Date Unavailable"}</p>
        {isSocialPostUri(post.uri) ? <Link href={socialPostPageHref(post.uri)} className="line-clamp-3 break-words hover:underline">{post.title || "View Post"}</Link> : <span className="line-clamp-3 break-words">{post.title || "Post Unavailable"}</span>}
      </td>
      {metrics.map(([key]) => <td key={key} className="px-3 py-3 tabular-nums">{metricLabel(post.engagement?.[key])}</td>)}
    </tr>)}</tbody>
  </table></div>;
}
