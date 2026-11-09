const knownLabels = ["ai-generated", "sexual"] as const;

/** Publisher-supplied document metadata, for display only. Unknown labels have no effect. */
export function normalizeArticleContentLabels(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const labels = knownLabels.filter(label => value.includes(label));
  return labels.length ? labels : undefined;
}

export function articleContentLabelFields(value: unknown): { contentLabels?: string[] } {
  const contentLabels = normalizeArticleContentLabels(value);
  return contentLabels ? { contentLabels } : {};
}

/** Only the canonical document's typed self-label field can supply publisher badges. */
export function documentContentLabels(record: Record<string, unknown>): string[] | undefined {
  if (record.$type !== "site.standard.document" || !record.labels || typeof record.labels !== "object" || Array.isArray(record.labels)) return undefined;
  const labels = record.labels as Record<string, unknown>;
  if (labels.$type !== "com.atproto.label.defs#selfLabels" || !Array.isArray(labels.values)) return undefined;
  return normalizeArticleContentLabels(labels.values.map(value => value && typeof value === "object" && !Array.isArray(value) ? value.val : undefined));
}
