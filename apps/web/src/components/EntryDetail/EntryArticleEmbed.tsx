"use client";

import { useCallback, useMemo, useState } from "react";
import { ExternalLink } from "lucide-react";
import { buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { normalizeHttpUrlToHttps } from "@/lib/publicResourceUrl";
import { cn } from "@/lib/utils";

interface EntryArticleEmbedProps {
  url: string;
  title: string;
  className?: string;
}

/** iframe embed of the canonical article URL with sandbox defaults and loading UI. */
export function EntryArticleEmbed({
  url,
  title,
  className,
}: EntryArticleEmbedProps) {
  const [loaded, setLoaded] = useState(false);
  const [failed, setFailed] = useState(false);

  const safeUrl = useMemo(() => normalizeHttpUrlToHttps(url), [url]);

  const handleLoad = useCallback(() => {
    setLoaded(true);
    setFailed(false);
  }, []);

  const handleError = useCallback(() => {
    setFailed(true);
    setLoaded(true);
  }, []);

  return (
    <div className={cn("flex flex-col gap-1.5 sm:gap-2", className)}>
      <div className="flex flex-wrap items-center gap-2 rounded-md border bg-muted/20 px-2 py-1.5 text-[11px] text-muted-foreground sm:rounded-lg sm:bg-muted/30 sm:px-3 sm:py-2 sm:text-xs">
        <div className="flex min-w-0 flex-1 basis-[min(100%,12rem)] items-center gap-1.5 sm:basis-auto sm:gap-2">
          <span className="hidden shrink-0 font-medium text-foreground sm:inline">
            Live Site
          </span>
          <a
            href={safeUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex min-h-[44px] min-w-0 flex-1 items-center gap-1 truncate py-1.5 hover:text-foreground hover:underline sm:min-h-0 sm:flex-none sm:py-0"
            title={safeUrl}
          >
            <ExternalLink className="size-3.5 shrink-0 sm:size-4" aria-hidden />
            <span className="truncate">{safeUrl}</span>
          </a>
        </div>
        <a
          href={safeUrl}
          target="_blank"
          rel="noopener noreferrer"
          className={cn(
            buttonVariants({ variant: "outline", size: "xs" }),
            "inline-flex min-h-[44px] min-w-[44px] items-center justify-center gap-1 sm:min-h-0 sm:min-w-0 sm:px-2"
          )}
          aria-label="Open in new tab"
        >
          <ExternalLink className="size-4 sm:hidden" aria-hidden />
          <span className="hidden sm:inline">Open</span>
        </a>
      </div>

      <div className="relative overflow-hidden rounded-lg border bg-background">
        {!loaded && !failed && (
          <div className="absolute inset-0 z-10 flex flex-col gap-2 p-3 sm:p-4">
            <Skeleton className="h-4 w-1/3" />
            <Skeleton className="h-[min(78vh,600px)] w-full rounded-md" />
          </div>
        )}
        {failed ? (
          <div className="flex min-h-[200px] items-center justify-center px-4 py-6 text-center text-sm text-muted-foreground">
            This page cannot be embedded (the site may block iframes). Use
            &quot;Open&quot; above or read the full content below.
          </div>
        ) : (
          <iframe
            title={`Embedded article: ${title}`}
            src={safeUrl}
            className={cn(
              "block h-[min(82vh,760px)] w-full bg-background",
              !loaded && "opacity-0"
            )}
            onLoad={handleLoad}
            onError={handleError}
            sandbox="allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox allow-forms"
            referrerPolicy="no-referrer-when-downgrade"
          />
        )}
      </div>
    </div>
  );
}
