/** True when the scroll container is not filled and another page may be needed. */
export function shouldFillViewportFetch(args: {
  scrollHeight: number;
  clientHeight: number;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
}): boolean {
  const { scrollHeight, clientHeight, hasNextPage, isFetchingNextPage } = args;
  if (!hasNextPage || isFetchingNextPage) return false;
  return scrollHeight <= clientHeight + 8;
}

/** True when the user has scrolled near the bottom of the entry list. */
export function shouldScrollNearEndFetch(args: {
  scrollTop: number;
  scrollHeight: number;
  clientHeight: number;
  threshold?: number;
}): boolean {
  const threshold = args.threshold ?? 320;
  return args.scrollHeight - args.scrollTop - args.clientHeight < threshold;
}
