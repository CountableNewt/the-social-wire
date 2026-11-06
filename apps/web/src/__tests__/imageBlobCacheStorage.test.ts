import { afterAll, afterEach, beforeAll, describe, expect, it, mock, spyOn } from "bun:test";
import { waitFor } from "@testing-library/react";
import { IDBFactory, IDBObjectStore } from "fake-indexeddb";

const originalIDB = Object.getOwnPropertyDescriptor(globalThis, "indexedDB");
const originalFetch = globalThis.fetch;
const dbName = "the-social-wire.image-cache.v1";
const modulePath = "../lib/imageBlobCache.ts?storage-performance";
let cache: typeof import("@/lib/imageBlobCache");
let legacy: IDBDatabase;
let db: IDBDatabase | undefined;
const restores: (() => void)[] = [];

function open(version: number): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(dbName, version);
    request.onupgradeneeded = () => request.result.createObjectStore("images", { keyPath: "url" });
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}
async function keys(): Promise<IDBValidKey[]> {
  db ??= await open(2);
  return new Promise((resolve, reject) => {
    const request = db!.transaction("images", "readonly").objectStore("images").getAllKeys();
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

beforeAll(async () => {
  Object.defineProperty(globalThis, "indexedDB", { configurable: true, value: new IDBFactory() });
  legacy = await open(1);
  await new Promise<void>((resolve, reject) => {
    const tx = legacy.transaction("images", "readwrite");
    for (let i = 0; i < 450; i++) tx.objectStore("images").put({
      url: `/images/old-${i}.png`, blob: new Blob(["original"], { type: "image/png" }),
      storedAt: Date.now() - 10_000 + i, contentType: "image/png",
    });
    tx.oncomplete = () => resolve(); tx.onabort = () => reject(tx.error);
  });
  cache = await import(modulePath) as typeof cache;
});
afterEach(() => { globalThis.fetch = originalFetch; restores.splice(0).forEach(restore => restore()); });
afterAll(() => {
  legacy.close(); db?.close();
  if (originalIDB) Object.defineProperty(globalThis, "indexedDB", originalIDB); else Reflect.deleteProperty(globalThis, "indexedDB");
});

describe("bounded image cache storage", () => {
  it("still displays images when an older tab blocks the cache upgrade", async () => {
    const fetchImage = mock(async () => new Response("image", { headers: { "Content-Type": "image/png" } }));
    globalThis.fetch = Object.assign(fetchImage, { preconnect: originalFetch.preconnect }) as typeof fetch;
    const objectUrl = await cache.fetchCachedImageObjectUrl("/images/blocked.png");
    expect(objectUrl).toStartWith("blob:");
    expect(fetchImage).toHaveBeenCalledTimes(1);
    URL.revokeObjectURL(objectUrl!);
    legacy.close();
  });

  it("upgrades existing images and evicts oldest keys without reading all cached blobs", async () => {
    const readAll = spyOn(IDBObjectStore.prototype, "getAll"); restores.push(() => readAll.mockRestore());
    const fetchImage = mock(async () => new Response("new", { headers: { "Content-Type": "image/png" } }));
    globalThis.fetch = Object.assign(fetchImage, { preconnect: originalFetch.preconnect }) as typeof fetch;
    const objectUrl = await cache.fetchCachedImageObjectUrl("/images/new.png");
    URL.revokeObjectURL(objectUrl!);
    await waitFor(async () => expect((await keys()).length).toBe(400));
    const remaining = await keys();
    expect(remaining).not.toContain("/images/old-0.png");
    expect(remaining).not.toContain("/images/old-50.png");
    expect(remaining).toContain("/images/old-100.png");
    expect(remaining).toContain("/images/new.png");
    expect(readAll).not.toHaveBeenCalled();
    const cachedUrl = await cache.fetchCachedImageObjectUrl("/images/old-449.png");
    expect(cachedUrl).toStartWith("blob:");
    URL.revokeObjectURL(cachedUrl!);
    expect(fetchImage).toHaveBeenCalledTimes(1);
  });

  it("keeps the cache bounded as a large batch of images arrives concurrently", async () => {
    const readAll = spyOn(IDBObjectStore.prototype, "getAll"); restores.push(() => readAll.mockRestore());
    globalThis.fetch = Object.assign(mock(async () => new Response("new", { headers: { "Content-Type": "image/png" } })), { preconnect: originalFetch.preconnect }) as typeof fetch;
    const urls = await Promise.all(Array.from({ length: 30 }, (_, i) => cache.fetchCachedImageObjectUrl(`/images/batch-${i}.png`)));
    urls.forEach(url => URL.revokeObjectURL(url!));
    await waitFor(async () => {
      const remaining = await keys();
      expect(remaining.length).toBe(400);
      expect(remaining.filter(key => String(key).startsWith("/images/batch-")).length).toBe(30);
    });
    expect(readAll).not.toHaveBeenCalled();
  });
});
