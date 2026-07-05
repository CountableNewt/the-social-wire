import type { NextRequest } from "next/server";
import { NextResponse } from "next/server";

import { getAppEnv } from "@/lib/appEnv";
import {
  buildLatrGatewayServerAuthHeaders,
  hasLatrGatewayServerCredentials,
  LATR_GATEWAY_PROXY_FORWARDED_REQUEST_HEADERS,
  LATR_GATEWAY_PROXY_FORWARDED_RESPONSE_HEADERS,
  LATR_GATEWAY_UPSTREAM_DPOP_HEADER,
  latrGatewayServerCredentialsHelpText,
  latrGatewayUpstreamBaseUrl,
} from "@/lib/latrGatewayProxyServer";

export const runtime = "nodejs";

type RouteContext = { params: Promise<{ path: string[] }> };

function authorizationScheme(value: string | null): string {
  const trimmed = value?.trim();
  if (!trimmed) return "missing";
  const separator = trimmed.indexOf(" ");
  return separator > 0 ? trimmed.slice(0, separator) : "present";
}

async function proxyLatrGateway(
  request: NextRequest,
  context: RouteContext
): Promise<NextResponse> {
  if (!hasLatrGatewayServerCredentials()) {
    return NextResponse.json(
      {
        error: "missing_client_credential",
        message: latrGatewayServerCredentialsHelpText(),
      },
      { status: 503 }
    );
  }

  const { path } = await context.params;
  const upstreamPath = `/${path.join("/")}${request.nextUrl.search}`;
  const upstreamUrl = `${latrGatewayUpstreamBaseUrl()}${upstreamPath}`;

  const headers = new Headers();
  for (const name of LATR_GATEWAY_PROXY_FORWARDED_REQUEST_HEADERS) {
    const value = request.headers.get(name);
    if (!value) continue;
    if (name === "authorization") {
      headers.set("Authorization", value);
    } else if (name === LATR_GATEWAY_UPSTREAM_DPOP_HEADER) {
      headers.set("DPoP", value);
    } else if (name === "x-atproto-upstream-dpop") {
      headers.set("X-ATProto-Upstream-DPoP", value);
    } else if (name === "content-type") {
      headers.set("Content-Type", value);
    } else if (name === "accept") {
      headers.set("Accept", value);
    } else {
      headers.set(name, value);
    }
  }
  for (const [name, value] of Object.entries(buildLatrGatewayServerAuthHeaders())) {
    headers.set(name, value);
  }
  const authDebug = {
    inAuth: authorizationScheme(request.headers.get("authorization")),
    inDpop: request.headers.has("dpop") ? "present" : "missing",
    inLatrDpop: request.headers.has(LATR_GATEWAY_UPSTREAM_DPOP_HEADER)
      ? "present"
      : "missing",
    inUpstreamDpop: request.headers.has("x-atproto-upstream-dpop")
      ? "present"
      : "missing",
    outAuth: authorizationScheme(headers.get("Authorization")),
    outDpop: headers.has("DPoP") ? "present" : "missing",
    outUpstreamDpop: headers.has("X-ATProto-Upstream-DPoP")
      ? "present"
      : "missing",
  };

  const body =
    request.method === "GET" || request.method === "HEAD"
      ? undefined
      : await request.arrayBuffer();

  let upstream: Response;
  try {
    upstream = await fetch(upstreamUrl, {
      method: request.method,
      headers,
      body,
    });
  } catch {
    return NextResponse.json(
      { error: "gateway_unreachable", message: "L@tr gateway request failed." },
      { status: 502 }
    );
  }

  const responseHeaders = new Headers();
  for (const name of LATR_GATEWAY_PROXY_FORWARDED_RESPONSE_HEADERS) {
    const value = upstream.headers.get(name);
    if (value) responseHeaders.set(name, value);
  }

  const upstreamText = await upstream.text();
  if (upstream.status >= 400 && getAppEnv() !== "prod") {
    responseHeaders.set(
      "X-Latr-Proxy-Auth-Debug",
      Object.entries(authDebug)
        .map(([key, value]) => `${key}:${value}`)
        .join(";")
    );
    try {
      const upstreamError = (JSON.parse(upstreamText) as { error?: string }).error?.trim();
      if (upstreamError) {
        responseHeaders.set("X-Latr-Upstream-Error", upstreamError);
      }
    } catch {
      /* ignore non-JSON bodies */
    }
  }

  return new NextResponse(upstreamText, {
    status: upstream.status,
    headers: responseHeaders,
  });
}

export async function GET(request: NextRequest, context: RouteContext) {
  return proxyLatrGateway(request, context);
}

export async function POST(request: NextRequest, context: RouteContext) {
  return proxyLatrGateway(request, context);
}

export async function PATCH(request: NextRequest, context: RouteContext) {
  return proxyLatrGateway(request, context);
}

export async function DELETE(request: NextRequest, context: RouteContext) {
  return proxyLatrGateway(request, context);
}
