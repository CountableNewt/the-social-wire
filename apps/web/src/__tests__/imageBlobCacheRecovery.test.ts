import { afterEach, beforeEach, expect, it, mock, spyOn } from "bun:test";
import { waitFor } from "@testing-library/react";
import { IDBFactory, IDBObjectStore } from "fake-indexeddb";

const originalIDB = Object.getOwnPropertyDescriptor(globalThis, "indexedDB");
const originalFetch = globalThis.fetch;
const originalCreate = URL.createObjectURL;
let sequence = 0;
let cache: typeof import("@/lib/imageBlobCache");
let db: IDBDatabase;
let created: Blob[];

beforeEach(async () => {
  Object.defineProperty(globalThis, "indexedDB", { configurable: true, value: new IDBFactory() });
  const modulePath = `../lib/imageBlobCache.ts?recovery-${++sequence}`;
  cache = await import(modulePath) as typeof cache;
  created = [];
  URL.createObjectURL = blob => { created.push(blob as Blob); return `blob:recovery-${created.length}`; };
  db = await new Promise((resolve, reject) => {
    const request = indexedDB.open("the-social-wire.image-cache.v1", 2);
    request.onupgradeneeded = () => request.result.createObjectStore("images", { keyPath: "url" }).createIndex("storedAt", "storedAt");
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
});
afterEach(() => {
  db.close(); globalThis.fetch = originalFetch; URL.createObjectURL = originalCreate;
  if (originalIDB) Object.defineProperty(globalThis, "indexedDB", originalIDB); else Reflect.deleteProperty(globalThis, "indexedDB");
});

function fetchMock(implementation: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>) {
  const request = mock(implementation);
  globalThis.fetch = Object.assign(request, { preconnect: originalFetch.preconnect }) as typeof fetch;
  return request;
}
function image(body: string) { return new Response(body, { headers: { "Content-Type": "image/png" } }); }
async function read(url: string): Promise<{ blob: Blob } | undefined> {
  return new Promise((resolve, reject) => {
    const request = db.transaction("images", "readonly").objectStore("images").get(url);
    request.onsuccess = () => resolve(request.result); request.onerror = () => reject(request.error);
  });
}
async function seed(url: string, blob: Blob) {
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction("images", "readwrite");
    tx.objectStore("images").put({ url, blob, contentType: blob.type, storedAt: Date.now() });
    tx.oncomplete = () => resolve(); tx.onabort = () => reject(tx.error);
  });
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(yes => { resolve = yes; });
  return { promise, resolve };
}

it("never caches non-image 200 responses or empty image bodies", async () => {
  const request = fetchMock(async () => new Response("<html>not an image</html>", { headers: { "Content-Type": "text/html" } }));
  expect(await cache.fetchCachedImageObjectUrl("/poisoned")).toBeUndefined();
  expect(await cache.fetchCachedImageObjectUrl("/poisoned")).toBeUndefined();
  expect(request).toHaveBeenCalledTimes(2);
  expect(await read("/poisoned")).toBeUndefined();
  request.mockImplementation(async () => image(""));
  expect(await cache.fetchCachedImageObjectUrl("/empty")).toBeUndefined();
  expect(await read("/empty")).toBeUndefined();
  expect(created).toHaveLength(0);
});

it("deletes invalid existing cache rows before trying the network", async () => {
  await seed("/old-html", new Blob(["login page"], { type: "text/html" }));
  await seed("/old-empty", new Blob([], { type: "image/jpeg" }));
  const request = fetchMock(async () => new Response("offline", { status: 503 }));
  expect(await cache.fetchCachedImageObjectUrl("/old-html")).toBeUndefined();
  expect(await cache.fetchCachedImageObjectUrl("/old-empty")).toBeUndefined();
  expect(await read("/old-html")).toBeUndefined(); expect(await read("/old-empty")).toBeUndefined();
  expect(request).toHaveBeenCalledTimes(2);
});

it("invalidates a decode failure using the original CDN URL and fetches fresh original bytes", async () => {
  const url = "https://cdn.bsky.app/img/feed_thumbnail/plain/fixture";
  const key = `/api/bluesky-card-thumb?${new URLSearchParams({ url })}`;
  await seed(key, new Blob(["undecodable"], { type: "image/png" }));
  const request = fetchMock(async () => image("replacement"));
  await cache.fetchCachedImageObjectUrl(url);
  expect(request).not.toHaveBeenCalled();
  await cache.invalidateCachedImage(url);
  expect(await read(key)).toBeUndefined();
  await cache.fetchCachedImageObjectUrl(url, undefined, { reload: true });
  expect(request.mock.calls[0]![1]?.cache).toBe("reload");
  expect(await created.at(-1)!.text()).toBe("replacement");
  expect(created.at(-1)!.type).toBe("image/png");
});

it("bypasses a valid cache on reload and coalesces concurrent retry consumers", async () => {
  await seed("/retry", new Blob(["old"], { type: "image/png" }));
  const response = deferred<Response>();
  const request = fetchMock(() => response.promise);
  const first = cache.fetchCachedImageObjectUrl("/retry", undefined, { reload: true });
  const second = cache.fetchCachedImageObjectUrl("/retry", undefined, { reload: true });
  const ordinary = cache.fetchCachedImageObjectUrl("/retry");
  await waitFor(() => expect(request).toHaveBeenCalledTimes(1));
  response.resolve(image("fresh"));
  const urls = await Promise.all([first, second, ordinary]);
  expect(new Set(urls).size).toBe(3);
  expect(await Promise.all(created.map(blob => blob.text()))).toEqual(["fresh", "fresh", "fresh"]);
  await waitFor(async () => expect(await (await read("/retry"))!.blob.text()).toBe("fresh"));
});

it("displays fetched bytes before an optional cache write finishes", async () => {
  let completeWrite: (() => void) | undefined;
  const originalPut = IDBObjectStore.prototype.put;
  const put = spyOn(IDBObjectStore.prototype, "put").mockImplementation(function (this: IDBObjectStore, ...args) {
    const tx = this.transaction;
    tx.addEventListener("complete", event => {
      event.stopImmediatePropagation();
      completeWrite = () => tx.oncomplete?.call(tx, event);
    }, { once: true });
    return originalPut.apply(this, args);
  });
  try {
    const request = fetchMock(async () => image("ready to display"));
    expect(await cache.fetchCachedImageObjectUrl("/slow-storage")).toStartWith("blob:");
    expect(await created[0]!.text()).toBe("ready to display");
    await waitFor(() => expect(completeWrite).toBeDefined());
    await cache.fetchCachedImageObjectUrl("/slow-storage");
    expect(request).toHaveBeenCalledTimes(1);
    completeWrite!();
  } finally { put.mockRestore(); }
});

it("prevents an invalidated in-flight response from returning or overwriting the replacement", async () => {
  const old = deferred<Response>();
  const request = fetchMock(() => old.promise);
  const stale = cache.fetchCachedImageObjectUrl("/race");
  await waitFor(() => expect(request).toHaveBeenCalledTimes(1));
  await cache.invalidateCachedImage("/race");
  const fresh = deferred<Response>();
  request.mockImplementation(() => fresh.promise);
  const replacement = cache.fetchCachedImageObjectUrl("/race", undefined, { reload: true });
  await waitFor(() => expect(request).toHaveBeenCalledTimes(2));
  old.resolve(image("stale"));
  expect(await stale).toBeUndefined();
  const concurrent = cache.fetchCachedImageObjectUrl("/race");
  fresh.resolve(image("current"));
  await Promise.all([replacement, concurrent]);
  expect(request).toHaveBeenCalledTimes(2);
  await waitFor(async () => expect(await (await read("/race"))!.blob.text()).toBe("current"));
  expect(await Promise.all(created.map(blob => blob.text()))).toEqual(["current", "current"]);
});
