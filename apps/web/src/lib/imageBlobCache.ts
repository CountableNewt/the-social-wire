import { normalizeHttpUrlToHttps } from "@/lib/publicResourceUrl";

const DB_NAME = "the-social-wire.image-cache.v1";
const STORE = "images";
const DB_VERSION = 2;
const STORED_AT_INDEX = "storedAt";
const MAX_ENTRIES = 400;
const MAX_AGE_MS = 1000 * 60 * 60 * 24 * 7;

type CachedImageRow = {
  url: string;
  blob: Blob;
  storedAt: number;
  contentType: string;
};

let dbPromise: Promise<IDBDatabase> | null = null;

const inFlightImageFetches = new Map<string, Promise<Blob | undefined>>();

function openImageCacheDb(): Promise<IDBDatabase> {
  if (typeof indexedDB === "undefined") {
    return Promise.reject(new Error("IndexedDB unavailable"));
  }
  if (!dbPromise) {
    dbPromise = new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open(DB_NAME, DB_VERSION);
      request.onupgradeneeded = () => {
        const db = request.result;
        const store = db.objectStoreNames.contains(STORE)
          ? request.transaction!.objectStore(STORE)
          : db.createObjectStore(STORE, { keyPath: "url" });
        if (!store.indexNames.contains(STORED_AT_INDEX)) {
          store.createIndex(STORED_AT_INDEX, "storedAt");
        }
      };
      let blocked = false;
      request.onblocked = () => {
        blocked = true;
        reject(new Error("Image cache upgrade is blocked by another tab"));
      };
      request.onsuccess = () => {
        const db = request.result;
        if (blocked) { db.close(); return; }
        db.onversionchange = () => { db.close(); dbPromise = null; };
        resolve(db);
      };
      request.onerror = () => reject(request.error ?? new Error("IndexedDB open failed"));
    }).catch(error => { dbPromise = null; throw error; });
  }
  return dbPromise;
}

async function readCachedBlob(url: string): Promise<Blob | null> {
  try {
    const db = await openImageCacheDb();
    return await new Promise((resolve, reject) => {
      const tx = db.transaction(STORE, "readonly");
      const store = tx.objectStore(STORE);
      const req = store.get(url);
      req.onsuccess = () => {
        const row = req.result as CachedImageRow | undefined;
        if (!row?.blob) {
          resolve(null);
          return;
        }
        if (Date.now() - row.storedAt > MAX_AGE_MS) {
          resolve(null);
          return;
        }
        resolve(row.blob);
      };
      req.onerror = () => reject(req.error ?? new Error("IndexedDB read failed"));
    });
  } catch {
    return null;
  }
}

async function writeCachedBlob(url: string, blob: Blob): Promise<void> {
  try {
    const db = await openImageCacheDb();
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
  signal?: AbortSignal
): Promise<string | undefined> {
  const url = normalizeImageCacheKey(rawUrl);
  if (!url) return undefined;

  // Cross-origin `<img src>` does not need CORS; `fetch()` does. Known Bluesky CDN
  // URLs are normalized to the same-origin image proxy before reaching this branch.
  if (shouldUseDirectImageSrc(url)) {
    return url;
  }

  const blob = await fetchCachedImageBlob(url, signal);
  return blob ? URL.createObjectURL(blob) : undefined;
}

async function fetchCachedImageBlob(
  url: string,
  signal?: AbortSignal,
): Promise<Blob | undefined> {
  const inFlight = inFlightImageFetches.get(url);
  if (inFlight) return inFlight;

  const fetchPromise = (async (): Promise<Blob | undefined> => {
    const cached = await readCachedBlob(url);
    if (cached) return cached;

    const res = await fetch(imageFetchUrl(url), {
      signal,
      referrerPolicy: "no-referrer",
      cache: "force-cache",
    });
    if (!res.ok) return undefined;

    const blob = await res.blob();
    if (blob.size === 0) return undefined;

    void writeCachedBlob(url, blob);
    return blob;
  })();

  inFlightImageFetches.set(url, fetchPromise);
  try {
    return await fetchPromise;
  } finally {
    inFlightImageFetches.delete(url);
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
