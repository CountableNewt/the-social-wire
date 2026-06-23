import { describe, expect, it } from "bun:test";

import {
  loadSidebarPublicationTab,
  saveSidebarPublicationTab,
  SIDEBAR_PUBLICATION_TAB_STORAGE_KEY,
} from "@/lib/sidebarPublicationTabStorage";

describe("sidebarPublicationTabStorage", () => {
  it("defaults to subscribed when unset", () => {
    const storage = {
      store: {} as Record<string, string>,
      getItem(key: string) {
        return this.store[key] ?? null;
      },
      setItem(key: string, value: string) {
        this.store[key] = value;
      },
    };

    expect(loadSidebarPublicationTab(storage)).toBe("subscribed");
  });

  it("persists following tab choice", () => {
    const storage = {
      store: {} as Record<string, string>,
      getItem(key: string) {
        return this.store[key] ?? null;
      },
      setItem(key: string, value: string) {
        this.store[key] = value;
      },
    };

    saveSidebarPublicationTab(storage, "following");
    expect(storage.store[SIDEBAR_PUBLICATION_TAB_STORAGE_KEY]).toBe("following");
    expect(loadSidebarPublicationTab(storage)).toBe("following");
  });
});
