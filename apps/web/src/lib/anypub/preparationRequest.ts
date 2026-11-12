import { buildArticleNativeContent } from "@/lib/articles/articleNativeContent";
import type { ArticleHost, ArticleRecord } from "@/lib/articles/articlePublishingTypes";
import { isRecord, prepareArticleRecord } from "./prepareArticleRecord";

const MAX_INPUT = 8 * 1024 * 1024;
const MAX_OUTPUT = 12 * 1024 * 1024;
const INLINE_BODY_LIMIT = 100 * 1024;
const encoder = new TextEncoder();
const responseHeaders = { "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff" };

class BodyLimitError extends Error {}

async function readBody(request: Request): Promise<string> {
  const reader = request.body?.getReader();
  if (!reader) return "";
  const decoder = new TextDecoder("utf-8", { fatal: true });
  const chunks: string[] = [];
  let size = 0;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > MAX_INPUT) {
        await reader.cancel();
        throw new BodyLimitError();
      }
      chunks.push(decoder.decode(value, { stream: true }));
    }
    chunks.push(decoder.decode());
    return chunks.join("");
  } finally {
    reader.releaseLock();
  }
}

function isScheduledHost(host: unknown): host is Extract<ArticleHost, "leaflet" | "markpub" | "unknown"> {
  return host === "leaflet" || host === "markpub" || host === "unknown";
}

function prepare(input: ArticleRecord): ArticleRecord {
  switch (input.operation) {
    case "article": {
      const native = input.native;
      if (!isRecord(native) || !isScheduledHost(native.host) || typeof native.markdown !== "string"
        || (native.assets !== undefined && (!Array.isArray(native.assets) || native.assets.length !== 0))) {
        throw new Error("Expected Text Content for a Supported Publication.");
      }
      if (encoder.encode(native.markdown).length > 1_000_000) throw new Error("Article Markdown Exceeds 1 MB.");
      return { native: buildArticleNativeContent(native.host, native.markdown, []) };
    }
    case "offload": {
      const { host, content } = input;
      if (!isScheduledHost(host) || !isRecord(content)) throw new Error("Expected Content for a Supported Publication.");
      const body = host === "leaflet" && Array.isArray(content.pages) ? JSON.stringify(content.pages)
        : host !== "leaflet" && isRecord(content.text) ? content.text.markdown : undefined;
      if (typeof body !== "string") throw new Error("Invalid Article Content.");
      const bytes = encoder.encode(body).length;
      if (host !== "leaflet" && bytes > 1_000_000) throw new Error("Article Markdown Exceeds 1 MB.");
      const offload = bytes > INLINE_BODY_LIMIT ? {
        kind: host === "leaflet" ? "leaflet-pages" : "markpub-markdown",
        mimeType: host === "leaflet" ? "application/json" : "text/markdown",
        body,
      } : undefined;
      return { prepared: { content, offload } };
    }
    case "document": {
      const { previous = {}, clearFields = [] } = input;
      if (!isRecord(input.input) || !isRecord(previous) || !Array.isArray(clearFields)
        || !clearFields.every(field => typeof field === "string")) throw new Error("Expected a Document Record.");
      return { record: prepareArticleRecord(input.input, previous, clearFields) };
    }
    default:
      throw new Error("Unknown Preparation Operation.");
  }
}

function requestError(status: number, message: string): Response {
  return Response.json({ error: "InvalidRequest", message }, { status, headers: responseHeaders });
}

/** Prepare article data locally. This endpoint does not contact the PDS or a publishing service. */
export async function handlePreparationRequest(request: Request): Promise<Response> {
  if (request.headers.get("Content-Type")?.split(";")[0] !== "application/json") {
    return requestError(415, "Expected application/json.");
  }
  let input: unknown;
  try {
    input = JSON.parse(await readBody(request));
  } catch (error) {
    if (error instanceof BodyLimitError) return requestError(413, "Preparation Input Exceeds 8 MiB.");
    return requestError(400, "Expected One Valid JSON Object.");
  }
  if (!isRecord(input)) return requestError(400, "Expected One Valid JSON Object.");
  let output: string;
  try {
    output = JSON.stringify(prepare(input));
  } catch (error) {
    return requestError(400, error instanceof Error ? error.message : "Invalid Preparation Input.");
  }
  if (encoder.encode(output).length > MAX_OUTPUT) return requestError(413, "Preparation Result Exceeds 12 MiB.");
  return new Response(output, { headers: { ...responseHeaders, "Content-Type": "application/json" } });
}
