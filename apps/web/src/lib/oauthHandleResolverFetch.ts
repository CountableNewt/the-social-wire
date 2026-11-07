const PRIMARY_RESOLVER = "https://public.api.bsky.app";
const FALLBACK_RESOLVER = "https://bsky.social";
const HANDLE_PATH = "/xrpc/com.atproto.identity.resolveHandle";

/** Retry only public identity reads; OAuth proofs and sandbox requests stay on their original host. */
export function createOAuthHandleResolverFetch(base: typeof fetch = fetch): typeof fetch {
  const wrapped = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const request = input instanceof Request ? input : undefined;
    const url = new URL(request?.url ?? String(input));
    const method = init?.method ?? request?.method ?? "GET";
    const headers = new Headers(init?.headers ?? request?.headers);
    const canRetry = url.origin === PRIMARY_RESOLVER && url.pathname === HANDLE_PATH
      && !url.username && !url.password && method.toUpperCase() === "GET"
      && !headers.has("Authorization") && !headers.has("DPoP");
    const signal = init?.signal ?? request?.signal;

    let response: Response;
    try {
      response = await base(input, init);
    } catch (error) {
      // Browser network/CORS failures reject with TypeError. Cancellation and
      // programming errors must not start another request.
      if (!canRetry || signal?.aborted || !(error instanceof TypeError)) throw error;
      return retry();
    }
    if (!canRetry || signal?.aborted || (response.status !== 429 && response.status < 500)) return response;
    await response.body?.cancel().catch(() => undefined);
    return retry();

    function retry(): Promise<Response> {
      signal?.throwIfAborted();
      url.host = new URL(FALLBACK_RESOLVER).host;
      return base(request ? new Request(url, request) : url, init);
    }
  };
  return Object.assign(wrapped, { preconnect: base.preconnect }) as typeof fetch;
}
