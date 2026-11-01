/** Temporarily suspended; opt in only after rebuilding the derived feed indices. */
export function algorithmicFeedsEnabled(): boolean {
  return process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED === "true";
}

export function isAlgorithmicFeed(feed: string | null): boolean {
  return feed === "wire" || feed === "circle" || feed === "finance" || feed === "sports";
}

export function availableReaderFeed(feed: string | null): string | null {
  return !algorithmicFeedsEnabled() && isAlgorithmicFeed(feed) ? "subscribed" : feed;
}
