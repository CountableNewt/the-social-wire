import { type NextRequest, NextResponse } from "next/server";
import { toolsEnabled } from "@/lib/toolsEnabled";
import { isPostSchedulingEnabled } from "@/lib/postScheduling";

/** Cover direct requests, nested routes, and client navigation to Tools. */
export function proxy(request: NextRequest) {
  const section = request.nextUrl.pathname.split("/")[1];
  // Articles has its own authenticated layout and entitlement checks.
  if (section === "articles") return NextResponse.next();
  const scheduling = section === "content-calendar" || section === "scheduling";
  if (!toolsEnabled() || (scheduling && !isPostSchedulingEnabled())) {
    return new NextResponse("Not Found", {
      status: 404,
      headers: { "Cache-Control": "private, no-store" },
    });
  }
  return NextResponse.next();
}

export const config = {
  matcher: [
    "/articles/:path*",
    "/live-post/:path*",
    "/livestream/:path*",
    "/brand-accounts/:path*",
    "/content-calendar/:path*",
    "/analytics/:path*",
    "/scheduling/:path*",
  ],
};
