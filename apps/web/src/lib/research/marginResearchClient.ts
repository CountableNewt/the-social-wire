import type { OAuthSession } from "@atproto/oauth-client-browser";
import { createOAuthAgent } from "@/lib/atprotoClient";

export type MarginAnnotation = {
  uri: string;
  motivation: string;
  source: string;
  title?: string;
  body?: string;
  quote?: string;
  tags: string[];
  createdAt: string;
  modifiedAt?: string;
};

const COLLECTION = "at.margin.note";
const object = (value: unknown): Record<string, unknown> => value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
const text = (value: unknown): string | undefined => typeof value === "string" && value.trim() ? value.trim() : undefined;

function exactQuote(value: unknown, depth = 0): string | undefined {
  if (depth > 16) return undefined;
  if (Array.isArray(value)) {
    for (const selector of value) {
      const quote = exactQuote(selector, depth + 1);
      if (quote) return quote;
    }
    return undefined;
  }
  const selector = object(value);
  return text(selector.exact) ?? exactQuote(selector.refinedBy, depth + 1);
}

export function parseMarginAnnotation(uri: string, value: unknown, did: string): MarginAnnotation | null {
  const prefix = `at://${did}/${COLLECTION}/`;
  if (!uri.startsWith(prefix) || !/^[a-zA-Z0-9._~:-]+$/.test(uri.slice(prefix.length))) return null;
  const record = object(value), target = object(record.target);
  const motivation = text(record.motivation), source = text(target.source), createdAt = text(record.createdAt);
  if (record.$type !== COLLECTION || !motivation || !source || !createdAt || !Number.isFinite(Date.parse(createdAt))) return null;
  return { uri, motivation, source, title: text(target.title), body: text(object(record.body).value), quote: exactQuote(target.selector), tags: Array.isArray(record.tags) ? record.tags.flatMap(value => text(value) ? [text(value)!] : []) : [], createdAt, modifiedAt: text(record.modifiedAt) };
}

export async function listMarginAnnotations(session: OAuthSession, signal?: AbortSignal): Promise<MarginAnnotation[]> {
  const agent = createOAuthAgent(session);
  const annotations = new Map<string, MarginAnnotation>();
  const cursors = new Set<string>();
  let cursor: string | undefined;
  for (let page = 0; page < 100; page++) {
    signal?.throwIfAborted();
    const { data } = await agent.com.atproto.repo.listRecords({ repo: session.did, collection: COLLECTION, limit: 100, cursor }, { signal });
    signal?.throwIfAborted();
    for (const record of data.records) {
      const annotation = parseMarginAnnotation(record.uri, record.value, session.did);
      if (annotation) annotations.set(annotation.uri, annotation);
    }
    cursor = data.cursor?.trim() || undefined;
    if (!cursor) return [...annotations.values()].sort((a, b) => {
      const date = (value: MarginAnnotation) => Date.parse(value.modifiedAt ?? "") || Date.parse(value.createdAt);
      return date(b) - date(a) || a.uri.localeCompare(b.uri);
    });
    if (cursors.has(cursor)) throw new Error("The PDS repeated a Margin pagination cursor.");
    cursors.add(cursor);
  }
  throw new Error("Margin returned too many pages. Please try again later.");
}
