import { normalizeArticleContentLabels } from "@/lib/articleContentLabels";

export function ArticleContentLabels({ contentLabels }: { contentLabels?: string[] }) {
  const labels = normalizeArticleContentLabels(contentLabels);
  if (!labels) return null;
  return <span className="inline-flex flex-wrap items-center gap-1">
    {labels.map(label => <span key={label} title={`${label === "ai-generated" ? "Made With AI" : "NSFW"} · Labeled by the Publisher`} className="inline-flex rounded-full border px-1.5 py-0.5 text-[10px] font-semibold leading-none text-muted-foreground">
      {label === "ai-generated" ? "AI" : "NSFW"}
    </span>)}
  </span>;
}
