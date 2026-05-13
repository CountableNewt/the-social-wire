"use client";

import { useCallback, useMemo, useState } from "react";
import { ExternalLink } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { sanitizeEmbedUrlForIframe } from "@/lib/publicResourceUrl";
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

  const iframeSrc = useMemo(() => sanitizeEmbedUrlForIframe(url), [url]);

  const handleLoad = useCallback(() => {
    setLoaded(true);
    setFailed(false);
  }, []);

  const handleError = useCallback(() => {
    setFailed(true);
    setLoaded(true);
  }, []);

  return (
    <div className={cn("relative w-full overflow-hidden rounded-lg", className)}>
      {!loaded && !failed && (
        <div className="absolute inset-0 z-10 flex flex-col gap-2 bg-background p-3 sm:p-4">
          <Skeleton className="h-[min(82vh,760px)] w-full rounded-md" />
        </div>
      )}
      {failed ? (
        <div className="flex min-h-[200px] flex-col items-center justify-center gap-3 px-4 py-6 text-center text-sm text-muted-foreground">
          <p>
            This page cannot be embedded (the site may block iframes). Open it
            in a new tab or read the full content below.
          </p>
          <a
            href={iframeSrc}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex min-h-[44px] items-center gap-1.5 rounded-md px-3 py-2 text-sm font-medium text-primary hover:underline"
          >
            <ExternalLink className="size-4 shrink-0" aria-hidden />
            Open in new tab
          </a>
        </div>
      ) : (
        <iframe
          title={`Embedded article: ${title}`}
          src={iframeSrc}
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
  );
}
