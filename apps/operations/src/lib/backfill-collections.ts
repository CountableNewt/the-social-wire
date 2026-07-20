export const BACKFILL_COLLECTION_OPTIONS = [
  "site.standard.document",
  "site.standard.entry",
  "app.skyreader.feed.subscription",
  "site.standard.graph.subscription",
  "com.standard.document",
  "com.standard.entry",
] as const

export function initialBackfillCollections(collections: readonly string[]) {
  return [...collections]
}

export function gapCollectionScopeLabel(collections: readonly string[]) {
  return collections.length > 0 ? collections.length.toLocaleString() : "Unknown"
}
