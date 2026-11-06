import { normalizeHttpUrlToHttps } from "@/lib/publicResourceUrl";

const DB_NAME = "the-social-wire.image-cache.v1";
const STORE = "images";
const DB_VERSION = 2;
const STORED_AT_INDEX = "storedAt";
const MAX_ENTRIES = 400;
const MAX_AGE_MS = 1000 * 60 * 60 * 24 * 7;
const CACHE_OPEN_TIMEOUT_MS = 250;

type CachedImageRow = {
  url: string;
  blob: Blob;
  storedAt: number;
  contentType: string;
};

let dbPromise: Promise<IDBDatabase> | null = null;
let abandonedOpen: IDBOpenDBRequest | null = null;

type ImageFetch = { reload: boolean; promise: Promise<Blob | undefined>; persistence?: Promise<void> };
const inFlightImageFetches = new Map<string, ImageFetch>();
const pendingInvalidations = new Map<string, Promise<void>>();

function isImageContentType(type: string): boolean {
  return /^image\/[a-z0-9][a-z0-9.+-]*$/i.test(type.split(";")[0].trim());
}

function isImageBlob(blob: Blob | undefined): blob is Blob {
  return !!blob && blob.size > 0 && typeof blob.type === "string" && isImageContentType(blob.type);
}

function isUsableRow(row: CachedImageRow | undefined): row is CachedImageRow {
  return !!row && isImageBlob(row.blob) && typeof row.contentType === "string" && isImageContentType(row.contentType) && Number.isFinite(row.storedAt) && Date.now() - row.storedAt <= MAX_AGE_MS;
}

async function deleteCachedBlob(url: string, invalidOnly = false): Promise<void> {
  try {
    const db = await openImageCacheDb();
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(STORE, "readwrite"), store = tx.objectStore(STORE);
      if (invalidOnly) {
        const request = store.get(url);
        request.onsuccess = () => { if (!isUsableRow(request.result)) store.delete(url); };
      } else store.delete(url);
      tx.oncomplete = () => resolve();
      tx.onabort = () => reject(tx.error ?? new Error("IndexedDB deletion failed"));
    });
  } catch { /* Image caching is optional. */ }
}

function openImageCacheDb(): Promise<IDBDatabase> {
  if (typeof indexedDB === "undefined") {
    return Promise.reject(new Error("IndexedDB unavailable"));
  }
  // IndexedDB open requests cannot be canceled. Once blocked or timed out,
  // skip this optional cache rather than queue more opens behind that request.
  if (abandonedOpen) return Promise.reject(new Error("Image cache temporarily unavailable"));
  if (!dbPromise) {
    const pending = new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open(DB_NAME, DB_VERSION);
      let abandoned = false;
      const abandon = (message: string) => {
        abandoned = true;
        abandonedOpen = request;
        clearTimeout(deadline);
        reject(new Error(message));
      };
      const deadline = setTimeout(() => abandon("Image cache open timed out"), CACHE_OPEN_TIMEOUT_MS);
      request.onupgradeneeded = () => {
        const db = request.result;
        const store = db.objectStoreNames.contains(STORE)
          ? request.transaction!.objectStore(STORE)
          : db.createObjectStore(STORE, { keyPath: "url" });
        if (!store.indexNames.contains(STORED_AT_INDEX)) {
          store.createIndex(STORED_AT_INDEX, "storedAt");
        }
      };
      request.onblocked = () => {
        abandon("Image cache upgrade is blocked by another tab");
      };
      request.onsuccess = () => {
        clearTimeout(deadline);
        const db = request.result;
        if (abandoned) {
          db.close();
          if (abandonedOpen === request) abandonedOpen = null;
          return;
        }
        db.onversionchange = () => { db.close(); if (dbPromise === opening) dbPromise = null; };
        resolve(db);
      };
      request.onerror = () => {
        clearTimeout(deadline);
        if (abandonedOpen === request) abandonedOpen = null;
        reject(request.error ?? new Error("IndexedDB open failed"));
      };
    });
    const opening = pending.catch(error => { if (dbPromise === opening) dbPromise = null; throw error; });
    dbPromise = opening;
  }
  return dbPromise;
}

async function readCachedBlob(url: string): Promise<Blob | null> {
  try {
    const db = await openImageCacheDb();
    const row = await new Promise<CachedImageRow | undefined>((resolve, reject) => {
      const tx = db.transaction(STORE, "readonly");
      const store = tx.objectStore(STORE);
      const req = store.get(url);
      req.onsuccess = () => {
        resolve(req.result as CachedImageRow | undefined);
      };
      req.onerror = () => reject(req.error ?? new Error("IndexedDB read failed"));
    });
    if (isUsableRow(row)) return row.blob;
    if (row) await deleteCachedBlob(url, true);
    return null;
  } catch {
    return null;
  }
}

async function writeCachedBlob(url: string, blob: Blob, isCurrent: () => boolean): Promise<void> {
  try {
    const db = await openImageCacheDb();
    if (!isCurrent()) return;
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(STORE, "readwrite");
      const store = tx.objectStore(STORE);
      store.put({
        url,
        blob,
        storedAt: Date.now(),
        contentType: blob.type || "application/octet-stream",
      } satisfies CachedImageRow);
      // Keep eviction inside the write transaction so concurrent image arrivals
      // share one consistent count. The index visits keys, never cached blobs.
      const count = store.count();
      count.onsuccess = () => {
        let excess = count.result - MAX_ENTRIES;
        if (excess <= 0) return;
        const oldest = store.index(STORED_AT_INDEX).openKeyCursor();
        oldest.onsuccess = () => {
          const cursor = oldest.result;
          if (!cursor || excess <= 0) return;
          store.delete(cursor.primaryKey);
          if (--excess > 0) cursor.continue();
        };
      };
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error ?? new Error("IndexedDB write failed"));
    });
  } catch {
    /* best-effort cache */
  }
}

function imageFetchUrl(url: string): string {
  if (typeof window === "undefined") return url;
  try {
    return new URL(url, window.location.href).href;
  } catch {
    return url;
  }
}

function normalizeImageCacheKey(raw: string): string | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const normalized = normalizeHttpUrlToHttps(trimmed);
  try {
    const url = new URL(normalized);
    if (url.hostname.toLowerCase() === "cdn.bsky.app") {
      const params = new URLSearchParams({ url: url.href });
      return `/api/bluesky-card-thumb?${params}`;
    }
  } catch {
    // Root-relative and malformed URLs continue through the normal cache path.
  }
  return normalized;
}

/** IndexedDB blob caching uses `fetch()`, which requires CORS. Most CDNs omit ACAO. */
function isSameOriginImageUrl(url: string): boolean {
  if (typeof window === "undefined") return false;
  try {
    return new URL(url, window.location.href).origin === window.location.origin;
  } catch {
    return false;
  }
}

export function isDirectImageLoadUrl(url: string): boolean {
  const normalized = normalizeImageCacheKey(url);
  if (!normalized) return false;
  return shouldUseDirectImageSrc(normalized);
}

/** Cross-origin images must use `<img src>`; only same-origin may use fetch + IndexedDB. */
export function shouldUseDirectImageSrc(normalizedUrl: string): boolean {
  if (normalizedUrl.startsWith("/mock-reader/")) return true;
  if (typeof window === "undefined") {
    try {
      const parsed = new URL(normalizedUrl);
      return parsed.protocol === "http:" || parsed.protocol === "https:";
    } catch {
      return false;
    }
  }
  return !isSameOriginImageUrl(normalizedUrl);
}

/** Returns a direct HTTPS `src` when blob caching would require CORS `fetch()`. */
export function resolveDirectImageUrl(rawUrl: string): string | undefined {
  const normalized = normalizeImageCacheKey(rawUrl);
  if (!normalized || !shouldUseDirectImageSrc(normalized)) return undefined;
  return normalized;
}

/**
 * Returns a blob object URL for `rawUrl`, using IndexedDB when available.
 * Caller must revoke the URL when the consumer unmounts.
 */
export async function fetchCachedImageObjectUrl(
  rawUrl: string,
  signal?: AbortSignal,
  options: { reload?: boolean } = {},
): Promise<string | undefined> {
  const url = normalizeImageCacheKey(rawUrl);
  if (!url) return undefined;

  // Cross-origin `<img src>` does not need CORS; `fetch()` does. Known Bluesky CDN
  // URLs are normalized to the same-origin image proxy before reaching this branch.
  if (shouldUseDirectImageSrc(url)) {
    return url;
  }

  const blob = await fetchCachedImageBlob(url, signal, options.reload ?? false);
  return blob ? URL.createObjectURL(blob) : undefined;
}

/** Drop a failed decode and retire its pending fetch before another attempt starts. */
export async function invalidateCachedImage(rawUrl: string): Promise<void> {
  const url = normalizeImageCacheKey(rawUrl);
  if (!url) return;
  inFlightImageFetches.delete(url);
  const previous = pendingInvalidations.get(url);
  const deletion = (async () => { await previous; await deleteCachedBlob(url); })();
  pendingInvalidations.set(url, deletion);
  try { await deletion; }
  finally { if (pendingInvalidations.get(url) === deletion) pendingInvalidations.delete(url); }
}

async function fetchCachedImageBlob(
  url: string,
  signal?: AbortSignal,
  reload = false,
): Promise<Blob | undefined> {
  const inFlight = inFlightImageFetches.get(url);
  if (inFlight && (!reload || inFlight.reload)) return inFlight.promise;

  const entry: ImageFetch = { reload, promise: Promise.resolve(undefined) };
  const isCurrent = () => inFlightImageFetches.get(url) === entry;

  const fetchPromise = (async (): Promise<Blob | undefined> => {
    await pendingInvalidations.get(url);
    if (!isCurrent()) return undefined;
    const cached = reload ? null : await readCachedBlob(url);
    if (!isCurrent()) return undefined;
    if (cached) return cached;

    const res = await fetch(imageFetchUrl(url), {
      signal,
      referrerPolicy: "no-referrer",
      cache: reload ? "reload" : "force-cache",
    });
    if (!res.ok || !isImageContentType(res.headers.get("Content-Type") ?? "")) return undefined;

    const blob = await res.blob();
    if (!isImageBlob(blob) || !isCurrent()) return undefined;

    entry.persistence = writeCachedBlob(url, blob, isCurrent);
    return blob;
  })();

  entry.promise = fetchPromise;
  inFlightImageFetches.set(url, entry);
  try {
    return await fetchPromise;
  } finally {
    const cleanup = () => { if (isCurrent()) inFlightImageFetches.delete(url); };
    if (entry.persistence) void entry.persistence.finally(cleanup);
    else cleanup();
  }
}

/** Warm the image cache without blocking UI (icons/thumbs after sidebar/feed load). */
export function prefetchCachedImages(urls: Iterable<string | null | undefined>): void {
  for (const raw of urls) {
    if (!raw?.trim()) continue;
    const url = normalizeImageCacheKey(raw);
    if (!url || !isSameOriginImageUrl(url)) continue;
    void fetchCachedImageObjectUrl(raw).then((objectUrl) => {
      if (objectUrl?.startsWith("blob:")) URL.revokeObjectURL(objectUrl);
    }).catch(() => {
      /* best-effort prefetch */
    });
  }
}
