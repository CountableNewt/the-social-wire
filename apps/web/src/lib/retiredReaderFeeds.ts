/** Global-ingestion feeds are retired in every environment, regardless of saved preferences. */
export function isRetiredReaderFeed(feed: string | null | undefined): boolean {
  return feed === "wire" || feed === "circle";
}
