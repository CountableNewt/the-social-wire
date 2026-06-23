import { NextResponse } from "next/server";

import { validateHttpsEmbedProbeTarget } from "@/lib/embedFramePolicy";

export const runtime = "nodejs";

const MAX_THUMB_BYTES = 1_000_000;

async function readLimitedBody(
  body: ReadableStream<Uint8Array>,
  maxBytes: number
): Promise<Uint8Array | null> {
  const reader = body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    if (!value) continue;
    total += value.byteLength;
    if (total > maxBytes) {
      await reader.cancel();
      return null;
    }
    chunks.push(value);
  }

  const out = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    out.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return out;
}

export async function GET(request: Request) {
  const rawUrl = new URL(request.url).searchParams.get("url") ?? "";
  const validated = validateHttpsEmbedProbeTarget(rawUrl);
  if (!validated.ok) {
    return NextResponse.json({ error: "invalid_url" }, { status: 400 });
  }

  const upstream = await fetch(validated.url, {
    headers: {
      Accept: "image/avif,image/webp,image/png,image/jpeg,image/*;q=0.8,*/*;q=0.1",
      "User-Agent": "The Social Wire Bluesky card thumbnail fetcher",
    },
    redirect: "follow",
  });

  if (!upstream.ok || !upstream.body) {
    return NextResponse.json({ error: "fetch_failed" }, { status: 502 });
  }

  const contentType =
    upstream.headers.get("content-type")?.split(";")[0]?.trim().toLowerCase() ??
    "";
  if (!contentType.startsWith("image/")) {
    return NextResponse.json({ error: "not_image" }, { status: 415 });
  }

  const contentLength = Number(upstream.headers.get("content-length") ?? "0");
  if (Number.isFinite(contentLength) && contentLength > MAX_THUMB_BYTES) {
    return NextResponse.json({ error: "too_large" }, { status: 413 });
  }

  const body = await readLimitedBody(upstream.body, MAX_THUMB_BYTES);
  if (!body) {
    return NextResponse.json({ error: "too_large" }, { status: 413 });
  }

  const arrayBuffer = new ArrayBuffer(body.byteLength);
  new Uint8Array(arrayBuffer).set(body);
  return new Response(arrayBuffer, {
    headers: {
      "Cache-Control": "public, max-age=3600",
      "Content-Length": String(body.byteLength),
      "Content-Type": contentType,
    },
  });
}
