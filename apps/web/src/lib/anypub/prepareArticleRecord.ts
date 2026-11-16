import { isValidAtUri, isValidNsid, isValidRecordKey } from "@atproto/syntax";
import type { ArticleRecord } from "@/lib/articles/articlePublishingTypes";

const unsafePathCharacters = /[\\?#:%\p{White_Space}\p{Cc}\p{Cf}]/u;

function isValidArticleRecordPath(path: string): boolean {
  try {
    const decoded = decodeURIComponent(path);
    return !!decoded && !decoded.startsWith("//") && !unsafePathCharacters.test(decoded)
      && !decoded.split("/").some(part => part === "." || part === "..");
  } catch {
    return false;
  }
}

export function isRecord(value: unknown): value is ArticleRecord {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function validSite(site: string): boolean {
  if (isValidAtUri(site)) {
    const publication = /^at:\/\/[^/]+\/site\.standard\.publication\/([^/]+)$/.exec(site);
    return publication !== null && isValidRecordKey(publication[1]);
  }
  if (!/^https:\/\//i.test(site) || /[\\?#\p{White_Space}\p{Cc}\p{Cf}]/u.test(site)) return false;
  try {
    const url = new URL(site);
    return !!url.hostname && !url.username && !url.password && !site.slice(8).split("/")[0].includes("@");
  } catch {
    return false;
  }
}

function validateText(value: unknown, field: string, maxBytes?: number, maxGraphemes?: number): asserts value is string {
  if (typeof value !== "string") throw new Error(`${field} Must Be Text.`);
  if (maxBytes && new TextEncoder().encode(value).length > maxBytes) throw new Error(`${field} Exceeds ${maxBytes} UTF-8 Bytes.`);
  if (maxGraphemes && [...new Intl.Segmenter(undefined, { granularity: "grapheme" }).segment(value)].length > maxGraphemes) {
    throw new Error(`${field} Exceeds ${maxGraphemes} Graphemes.`);
  }
}

function validTimestamp(value: unknown): boolean {
  if (typeof value !== "string") return false;
  const match = /^(\d{4}-\d{2}-\d{2}T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d)(?:\.\d{1,9})?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$/.exec(value);
  if (!match || match[0] !== value) return false;
  // Date parsing normalizes impossible days. Round-trip the local calendar fields to detect them.
  const local = new Date(`${match[1]}Z`);
  return Number.isFinite(local.getTime()) && local.toISOString().startsWith(match[1]);
}

export function prepareArticleRecord(input: ArticleRecord, previous: ArticleRecord, clearFields: string[]): ArticleRecord {
  const retained: ArticleRecord = { ...previous };
  for (const key of clearFields) delete retained[key];
  const document: ArticleRecord = { ...retained, ...input, $type: "site.standard.document" };
  validateText(document.site, "Site");
  if (!validSite(document.site)) throw new Error("Choose a Standard.site Publication URI or HTTPS URL.");
  validateText(document.title, "Title", 5000, 500);
  if (!validTimestamp(document.publishedAt) || ("updatedAt" in document && !validTimestamp(document.updatedAt))) {
    throw new Error("Choose a Valid Publication Timestamp.");
  }
  if ("description" in document) validateText(document.description, "Description", 30000, 3000);
  if ("textContent" in document) validateText(document.textContent, "Text Content");
  if ("path" in document) {
    validateText(document.path, "Path");
    if (!isValidArticleRecordPath(document.path)) throw new Error("Choose a Relative Article Path Without Query Parameters or Traversal.");
  }
  for (const key of ["tags", "langs"]) {
    if (!(key in document)) continue;
    const values = document[key];
    if (!Array.isArray(values)) throw new Error(`${key} Must Be an Array of Strings.`);
    for (const value of values) {
      if (key === "tags") validateText(value, "Tag", 1280, 128);
      else validateText(value, "Language");
    }
  }
  if ("content" in document) {
    const type = isRecord(document.content) ? document.content.$type : undefined;
    if (typeof type !== "string" || !isValidNsid(type.split("#")[0]) || !/^[^#]+(?:#(?!main$)[a-zA-Z][a-zA-Z0-9_]*)?$/.test(type)) {
      throw new Error("Content Must Have a Valid Lexicon Type.");
    }
  }
  return document;
}
