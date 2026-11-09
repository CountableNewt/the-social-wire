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
  return <ul aria-label="Profile Links" tabIndex={0} className="flex w-full min-w-0 max-w-xl gap-2 overflow-x-auto px-4 py-1 text-left focus-visible:outline-2 focus-visible:outline-ring">
    {query.data.map(link => <li key={link.uri} className="w-max shrink-0">
      <a href={link.url} title={link.title || link.domain} target="_blank" rel="noopener noreferrer" onFocus={event => event.currentTarget.scrollIntoView({ block: "nearest", inline: "nearest" })} className="flex h-full min-h-11 min-w-0 items-center gap-2 rounded-lg border bg-card px-2 py-1.5 hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring focus-visible:-outline-offset-2">
        <span className="relative flex size-6 shrink-0 items-center justify-center overflow-hidden rounded-md bg-muted">
          <LinkIcon aria-hidden="true" className="size-3 text-muted-foreground" />
          {link.iconUrl ? <CachedImage src={link.iconUrl} alt="" width={24} height={24} loading="eager" aria-hidden={true} className="absolute inset-0 size-6 bg-card object-contain" /> : null}
        </span>
        <span className="whitespace-nowrap"><span className="block text-xs font-medium leading-4">{link.title || link.domain}</span>{link.title ? <span className="block text-[11px] leading-3 text-muted-foreground">{link.domain}</span> : null}</span>
        <ExternalLink aria-hidden="true" className="size-3 shrink-0 text-muted-foreground" />
      </a>
    </li>)}
  </ul>;
}
