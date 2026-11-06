import { ExternalLink } from "lucide-react";

export function ResearchSourceLink({ url }: { url?: string }) {
  if (!url) return null;
  try {
    const parsed = new URL(url);
    if (!["http:", "https:"].includes(parsed.protocol)) return null;
  } catch { return null; }
  return <a href={url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 text-sm text-primary hover:underline">Open Source<ExternalLink className="size-3" aria-hidden="true" /></a>;
}
