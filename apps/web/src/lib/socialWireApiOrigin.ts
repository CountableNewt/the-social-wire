/** Keep hosted browsers on their own Services deployment, even with stale build env. */
export function resolveSocialWireApiOrigin(browserOrigin?: string, localOverride?: string): string {
  if (browserOrigin) {
    const origin = new URL(browserOrigin);
    const loopback = origin.hostname === "localhost" ||
      origin.hostname === "[::1]" || origin.hostname.startsWith("127.");
    if (!loopback || !localOverride?.trim()) return origin.origin;
  }

  const configured = localOverride?.trim();
  if (!configured) throw new Error("Social Wire API origin requires a browser origin or explicit local configuration");
  const url = new URL(configured);
  if (!/^https?:$/.test(url.protocol) || url.username || url.password ||
    url.pathname !== "/" || url.search || url.hash) {
    throw new Error("Social Wire API URL must be an HTTP(S) origin");
  }
  return url.origin;
}

export function socialWireApiOrigin(): string {
  // Client components can render on the server. Use this deployment's hostname
  // there; hydration resolves the active browser alias before any user action.
  const origin = typeof window !== "undefined"
    ? window.location.origin
    : process.env.VERCEL_URL ? `https://${process.env.VERCEL_URL}` : "http://localhost:3000";
  return resolveSocialWireApiOrigin(
    origin,
    process.env.NEXT_PUBLIC_SOCIALWIRE_API_URL,
  );
}
