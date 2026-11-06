import { afterEach, beforeEach, expect, it, mock, spyOn } from "bun:test";
import { IDBFactory } from "fake-indexeddb";
import type { PersistedClient, Persister } from "@tanstack/react-query-persist-client";
import { createIndexedDbQueryPersister } from "@/lib/indexedDbQueryPersister";

const originalIDB = Object.getOwnPropertyDescriptor(window, "indexedDB");
const restores: (() => void)[] = [];
let timers: Map<number, () => void>;
let factory: IDBFactory;
function snapshot(buster: string): PersistedClient { return { timestamp: Date.now(), buster, clientState: { queries: [], mutations: [] } }; }
function flush() { const callbacks = [...timers.values()]; timers.clear(); callbacks.forEach(callback => callback()); }
async function restored(persister: Persister): Promise<PersistedClient | undefined> {
  let value = await persister.restoreClient();
  for (let attempt = 0; !value && attempt < 30; attempt++) {
    await new Promise(resolve => setTimeout(resolve, 0));
    value = await persister.restoreClient();
  }
  return value;
}
beforeEach(() => {
  factory = new IDBFactory(); timers = new Map();
  Object.defineProperty(window, "indexedDB", { configurable: true, value: factory });
  let id = 0;
  const previousSet = Object.getOwnPropertyDescriptor(window, "setTimeout");
  const previousClear = Object.getOwnPropertyDescriptor(window, "clearTimeout");
  Object.defineProperty(window, "setTimeout", { configurable: true, value: (callback: () => void) => { timers.set(++id, callback); return id; } });
  Object.defineProperty(window, "clearTimeout", { configurable: true, value: (value: number) => { timers.delete(value); } });
  restores.push(() => { if (previousSet) Object.defineProperty(window, "setTimeout", previousSet); if (previousClear) Object.defineProperty(window, "clearTimeout", previousClear); });
});
afterEach(() => {
  restores.splice(0).reverse().forEach(restore => restore());
  if (originalIDB) Object.defineProperty(window, "indexedDB", originalIDB); else Reflect.deleteProperty(window, "indexedDB");
});

it("prepares only the newest snapshot once per write window, including successive bursts", async () => {
  const prepare = mock((client: PersistedClient) => ({ ...client, buster: `prepared:${client.buster}` }));
  const persister = createIndexedDbQueryPersister({ dbName: "coalesced-query-cache", storeName: "cache", key: "snapshot", prepareClient: prepare });
  for (let event = 0; event < 100; event++) await persister.persistClient(snapshot(String(event)));
  expect(prepare).not.toHaveBeenCalled();
  expect(timers.size).toBe(1);
  flush();
  expect(prepare).toHaveBeenCalledTimes(1);
  expect(prepare.mock.calls[0][0].buster).toBe("99");
  expect((await restored(persister))?.buster).toBe("prepared:99");
  await persister.persistClient(snapshot("next"));
  flush();
  expect(prepare).toHaveBeenCalledTimes(2);
  expect((await restored(persister))?.buster).toBe("prepared:next");
});

it("cancels pending preparation and removes saved content without a late buffered rewrite", async () => {
  const prepare = mock((client: PersistedClient) => client);
  const persister = createIndexedDbQueryPersister({ dbName: "removed-query-cache", storeName: "cache", key: "snapshot", prepareClient: prepare });
  await persister.persistClient(snapshot("saved")); flush(); await restored(persister);
  await persister.persistClient(snapshot("buffered"));
  const expiredCallback = [...timers.values()][0];
  await persister.removeClient();
  expect(timers.size).toBe(0);
  expiredCallback();
  expect(prepare).toHaveBeenCalledTimes(1);
  expect(await persister.restoreClient()).toBeUndefined();
});

it("keeps preparation errors best effort and permits the next window to persist", async () => {
  const prepare = mock((client: PersistedClient): PersistedClient => { if (client.buster === "invalid") throw new Error("Cannot serialize"); return client; });
  const persister = createIndexedDbQueryPersister({ dbName: "failed-preparation-cache", storeName: "cache", key: "snapshot", prepareClient: prepare });
  await persister.persistClient(snapshot("invalid"));
  expect(() => flush()).not.toThrow();
  expect(await persister.restoreClient()).toBeUndefined();
  await persister.persistClient(snapshot("recovered")); flush();
  expect((await restored(persister))?.buster).toBe("recovered");
});

it("recovers from unavailable IndexedDB without retaining a failed write window", async () => {
  const persister = createIndexedDbQueryPersister({ dbName: "unavailable-query-cache", storeName: "cache", key: "snapshot" });
  const open = spyOn(factory, "open").mockImplementation(() => { throw new Error("Storage unavailable"); }); restores.push(() => open.mockRestore());
  await persister.persistClient(snapshot("offline-storage")); flush();
  expect(await persister.restoreClient()).toBeUndefined();
  open.mockRestore();
  await persister.persistClient(snapshot("recovered")); flush();
  expect((await restored(persister))?.buster).toBe("recovered");
});
