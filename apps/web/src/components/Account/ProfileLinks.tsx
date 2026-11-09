"use client";

import { useQuery } from "@tanstack/react-query";
import { ExternalLink, Link as LinkIcon } from "lucide-react";
import { loadProfileLinks } from "@/lib/profileLinks";
import { CachedImage } from "@/components/shared/CachedImage";
import { Button } from "@/components/ui/button";

export function ProfileLinks({ did }: { did: string }) {
  const query = useQuery({
    queryKey: ["actorProfileLinks", did],
    queryFn: ({ signal }) => loadProfileLinks(did, signal),
    enabled: !!did,
    staleTime: 5 * 60_000,
    retry: false,
  });
  if (query.isError) return <div className="flex flex-wrap items-center justify-center gap-2 px-4 text-xs text-muted-foreground">
    <p>Profile Links Could Not Be Loaded.</p><Button variant="ghost" size="sm" onClick={() => { void query.refetch(); }}>Retry Profile Links</Button>
  </div>;
  if (!query.data?.length) return null;
  return <ul aria-label="Profile Links" className="grid w-full max-w-xl gap-2 px-4 text-left sm:grid-cols-2">
    {query.data.map(link => <li key={link.uri} className="min-w-0">
      <a href={link.url} target="_blank" rel="noopener noreferrer" className="flex h-full min-w-0 items-center gap-3 rounded-xl border bg-card p-3 hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring">
        <span className="relative flex size-8 shrink-0 items-center justify-center overflow-hidden rounded-lg bg-muted">
          <LinkIcon aria-hidden="true" className="size-4 text-muted-foreground" />
          {link.iconUrl ? <CachedImage src={link.iconUrl} alt="" width={32} height={32} loading="eager" aria-hidden={true} className="absolute inset-0 size-8 bg-card object-contain" /> : null}
        </span>
        <span className="min-w-0 flex-1"><span className="block truncate text-sm font-medium">{link.title || link.domain}</span>{link.title ? <span className="block truncate text-xs text-muted-foreground">{link.domain}</span> : null}</span>
        <ExternalLink aria-hidden="true" className="size-3.5 shrink-0 text-muted-foreground" />
      </a>
    </li>)}
  </ul>;
}
