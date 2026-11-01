"use client";

import { usePathname, useSearchParams } from "next/navigation";
import { SidebarMenuSub, SidebarMenuSubButton, SidebarMenuSubItem } from "@/components/ui/sidebar";

export type SidebarTopicInterest = { id: string; title: string };

export function SidebarTopicInterestLinks({ topic, rows, loading, error, onNavigate }: {
  topic: "finance" | "sports";
  rows: readonly SidebarTopicInterest[];
  loading: boolean;
  error: boolean;
  onNavigate?: (href: string) => void;
}) {
  const pathname = usePathname();
  const search = useSearchParams();
  const selected = pathname === "/read" && search.get("feed") === topic ? search.get(`${topic}Feed`) : null;
  return <SidebarMenuSub className="pl-4" aria-label={topic === "finance" ? "Finance Interests" : "Sports Interests"}>
    {loading ? <SidebarMenuSubItem><p role="status" className="px-2 py-1 text-xs text-muted-foreground">Loading Your Interests…</p></SidebarMenuSubItem> : null}
    {error ? <SidebarMenuSubItem><p role="status" className="px-2 py-1 text-xs text-muted-foreground">Your interests could not load. Try Refresh.</p></SidebarMenuSubItem> : null}
    {rows.map(row => {
      const href = `/read?${new URLSearchParams({ feed: topic, [`${topic}Feed`]: row.id })}`;
      return <SidebarMenuSubItem key={row.id}><SidebarMenuSubButton
        render={<a href={href} onClick={event => {
          if (!onNavigate || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
          event.preventDefault();
          onNavigate(href);
        }} />}
        isActive={selected === row.id}
        aria-current={selected === row.id ? "page" : undefined}
        className="h-auto min-h-8 py-1 [&>span:last-child]:whitespace-normal [&>span:last-child]:break-words"
      ><span>{row.title}</span></SidebarMenuSubButton></SidebarMenuSubItem>;
    })}
    {!loading && !error && rows.length === 0 ? <SidebarMenuSubItem><p className="px-2 py-1 text-xs text-muted-foreground">Follow Interests in Customize.</p></SidebarMenuSubItem> : null}
  </SidebarMenuSub>;
}
