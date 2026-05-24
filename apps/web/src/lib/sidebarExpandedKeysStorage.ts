import {
  SIDEBAR_SEC_FOLDERS,
  SIDEBAR_SEC_PUBLICATIONS,
} from "@/components/AppSidebar/appSidebarConstants";

export const SIDEBAR_EXPANDED_KEYS_STORAGE_KEY =
  "the-social-wire.sidebar-expanded-keys.v1";

export const SIDEBAR_FOLDER_EXPAND_MIGRATE_EVENT =
  "the-social-wire:sidebar-folder-expand-migrate";

const FOLDER_EXPAND_PREFIX = "folder:";

export function folderExpandKey(rkey: string): string {
  return `${FOLDER_EXPAND_PREFIX}${rkey}`;
}

export function isFolderExpandKey(key: string): boolean {
  return key.startsWith(FOLDER_EXPAND_PREFIX);
}

export function rkeyFromFolderExpandKey(key: string): string | null {
  return isFolderExpandKey(key) ? key.slice(FOLDER_EXPAND_PREFIX.length) : null;
}

export function defaultSidebarExpandedKeys(): Set<string> {
  return new Set([SIDEBAR_SEC_FOLDERS, SIDEBAR_SEC_PUBLICATIONS]);
}

type ExpandedKeysStore = Record<string, string[]>;

function readStore(storage: Pick<Storage, "getItem">): ExpandedKeysStore {
  try {
    const raw = storage.getItem(SIDEBAR_EXPANDED_KEYS_STORAGE_KEY);
    if (!raw) return {};
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object") return {};
    return parsed as ExpandedKeysStore;
  } catch {
    return {};
  }
}

function writeStore(
  storage: Pick<Storage, "getItem" | "setItem">,
  store: ExpandedKeysStore
): void {
  try {
    storage.setItem(SIDEBAR_EXPANDED_KEYS_STORAGE_KEY, JSON.stringify(store));
  } catch {
    /* quota / private mode */
  }
}

export function loadSidebarExpandedKeys(
  storage: Pick<Storage, "getItem">,
  did: string
): Set<string> {
  if (!did) return defaultSidebarExpandedKeys();
  const keys = readStore(storage)[did];
  if (!keys?.length) return defaultSidebarExpandedKeys();
  return new Set(keys);
}

export function saveSidebarExpandedKeys(
  storage: Pick<Storage, "getItem" | "setItem">,
  did: string,
  keys: Iterable<string>
): void {
  if (!did) return;
  const store = readStore(storage);
  store[did] = [...keys];
  writeStore(storage, store);
}

export function migrateStoredSidebarFolderExpandKey(
  storage: Pick<Storage, "getItem" | "setItem">,
  did: string,
  oldRkey: string,
  newRkey: string
): void {
  if (!did || oldRkey === newRkey) return;

  const current = loadSidebarExpandedKeys(storage, did);
  const oldKey = folderExpandKey(oldRkey);
  if (!current.has(oldKey)) return;

  const next = new Set(current);
  next.delete(oldKey);
  next.add(folderExpandKey(newRkey));
  saveSidebarExpandedKeys(storage, did, next);

  if (typeof window !== "undefined") {
    try {
      window.dispatchEvent(
        new CustomEvent(SIDEBAR_FOLDER_EXPAND_MIGRATE_EVENT, {
          detail: { did, oldRkey, newRkey },
        })
      );
    } catch {
      /* jsdom and other minimal runtimes may lack CustomEvent */
    }
  }
}

/** Legacy expand keys used folder AT-URIs before rkey-based keys. */
export function migrateLegacyFolderUriExpandKeys(
  keys: Set<string>,
  folderUris: Iterable<string>
): Set<string> {
  let changed = false;
  const next = new Set(keys);
  for (const uri of folderUris) {
    if (!next.has(uri)) continue;
    next.delete(uri);
    const rkey = uri.split("/").pop();
    if (rkey) {
      next.add(folderExpandKey(rkey));
    }
    changed = true;
  }
  return changed ? next : keys;
}
