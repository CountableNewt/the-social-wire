import Link from "next/link";
import { metricLabel } from "@/lib/analytics/creatorAnalyticsClient";
import type { BlueskyAnalyticsPost } from "@/lib/analytics/blueskyAnalyticsClient";
import { isSocialPostUri, socialPostPageHref } from "@/lib/socialPostNavigation";

export function BlueskyPostTable({ posts }: { posts: BlueskyAnalyticsPost[] }) {
  return <div className="overflow-x-auto rounded-xl border"><table className="w-full text-left text-xs">
    <thead className="bg-muted/40"><tr>{["Post", "Likes", "Replies", "Reposts", "Quotes"].map(label => <th key={label} scope="col" className="whitespace-nowrap px-3 py-3 font-medium">{label}</th>)}</tr></thead>
    <tbody>{posts.map(post => <tr key={post.uri} className="border-t">
      <td className="min-w-48 max-w-96 px-3 py-3"><p className="mb-1 text-muted-foreground">{post.publishedAt && Number.isFinite(Date.parse(post.publishedAt)) ? new Date(post.publishedAt).toLocaleDateString() : "Date Unavailable"}</p>
        {isSocialPostUri(post.uri) ? <Link href={socialPostPageHref(post.uri)} className="line-clamp-3 break-words hover:underline">{post.title || "View Post"}</Link> : <span className="line-clamp-3 break-words">{post.title || "Post Unavailable"}</span>}
      </td>
      {[post.engagement?.likes, post.engagement?.replies, post.engagement?.reposts, post.engagement?.quotes].map((value, index) => <td key={index} className="px-3 py-3 tabular-nums">{metricLabel(value)}</td>)}
    </tr>)}</tbody>
  </table></div>;
}
