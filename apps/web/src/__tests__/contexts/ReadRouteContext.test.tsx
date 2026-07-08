import { describe, expect, it, mock } from "bun:test";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import React from "react";

import { ReadRouteProvider, useReadRoute } from "@/contexts/ReadRouteContext";
import { READ_STATE_STORAGE_KEY } from "@/lib/entryReadStateStorage";

mock.module("@/hooks/useAuth", () => ({
  useAuth: () => ({
    session: { did: "did:plc:viewer" },
    getOAuthSession: () => null,
  }),
}));

function makeWrapper() {
  const queryClient = new QueryClient();
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <ReadRouteProvider>{children}</ReadRouteProvider>
      </QueryClientProvider>
    );
  };
}

describe("ReadRouteProvider", () => {
  it("marks entries read and unread in local state", () => {
    const storage = {
      store: {} as Record<string, string>,
      getItem(key: string) {
        return this.store[key] ?? null;
      },
      setItem(key: string, value: string) {
        this.store[key] = value;
      },
    };

    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: storage,
    });
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: globalThis,
    });

    const entryId = "at://did:plc:author/site.standard.document/one";
    const { result } = renderHook(() => useReadRoute(), {
      wrapper: makeWrapper(),
    });

    expect(result.current.isEntryRead(entryId)).toBe(false);

    act(() => {
      result.current.markEntryRead(entryId, {
        publicationId: "at://did:plc:author/site.standard.publication/main",
      });
    });

    expect(result.current.isEntryRead(entryId)).toBe(true);
    expect(storage.store[READ_STATE_STORAGE_KEY] ?? "").toContain(entryId);

    act(() => {
      result.current.markEntryUnread(entryId, {
        publicationId: "at://did:plc:author/site.standard.publication/main",
      });
    });

    expect(result.current.isEntryRead(entryId)).toBe(false);
    expect(storage.store[READ_STATE_STORAGE_KEY]).not.toContain(entryId);
  });

  it("persists publication tab selection", () => {
    const storage = {
      store: {} as Record<string, string>,
      getItem(key: string) {
        return this.store[key] ?? null;
      },
      setItem(key: string, value: string) {
        this.store[key] = value;
      },
    };

    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: storage,
    });
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: globalThis,
    });

    const { result } = renderHook(() => useReadRoute(), {
      wrapper: makeWrapper(),
    });

    expect(result.current.publicationTab).toBe("subscribed");

    act(() => {
      result.current.setPublicationTab("following");
    });

    expect(result.current.publicationTab).toBe("following");
    expect(storage.store["the-social-wire.sidebar-publication-tab.v1"]).toBe(
      "following"
    );
  });
});
