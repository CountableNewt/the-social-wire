import type { PublicationTab } from "@/components/AppSidebar/appSidebarConstants";

export const SIDEBAR_PUBLICATION_TAB_STORAGE_KEY =
  "the-social-wire.sidebar-publication-tab.v1";

export function loadSidebarPublicationTab(
  storage: Pick<Storage, "getItem">
): PublicationTab {
  try {
    const raw = storage.getItem(SIDEBAR_PUBLICATION_TAB_STORAGE_KEY);
    return raw === "following" ? "following" : "subscribed";
  } catch {
    return "subscribed";
  }
}

export function saveSidebarPublicationTab(
  storage: Pick<Storage, "setItem">,
  tab: PublicationTab
): void {
  try {
    storage.setItem(SIDEBAR_PUBLICATION_TAB_STORAGE_KEY, tab);
  } catch {
    /* quota / private mode */
  }
}
