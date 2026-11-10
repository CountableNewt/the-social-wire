import { afterEach, expect, it, mock } from "bun:test";
import { act, cleanup, fireEvent, renderHook } from "@testing-library/react";
import { StrictMode, type ReactNode } from "react";
import { useDeferredArticleRead } from "@/hooks/useDeferredArticleRead";

afterEach(cleanup);

function fixture(attribute?: string, value?: string) {
  const element = document.createElement("button");
  if (attribute) element.setAttribute(attribute, value!);
  document.body.append(element);
  return element;
}

it("keeps the opened article unread through focus changes, rerenders, and repeated opens", () => {
  const markRead = mock();
  const { result, rerender } = renderHook(() => useDeferredArticleRead({ scope: "feed", viewerKey: "alice:1", markRead }));
  act(() => result.current.open("a"));
  window.dispatchEvent(new window.Event("blur"));
  window.dispatchEvent(new window.Event("focus"));
  document.dispatchEvent(new window.Event("visibilitychange"));
  rerender();
  act(() => result.current.open("a"));
  expect(markRead).not.toHaveBeenCalled();
  act(() => result.current.open("b"));
  expect(markRead.mock.calls).toEqual([["a"]]);
  act(() => result.current.finish());
  act(() => result.current.finish());
  expect(markRead.mock.calls).toEqual([["a"], ["b"]]);
});

it("marks the pending article on scope changes with its original publication callback", () => {
  const first = mock(), second = mock();
  const { result, rerender, unmount } = renderHook(({ scope, markRead }) => useDeferredArticleRead({ scope, viewerKey: "alice:1", markRead }), { initialProps: { scope: "one", markRead: first } });
  act(() => result.current.open("a"));
  rerender({ scope: "two", markRead: second });
  expect(first.mock.calls).toEqual([["a"]]);
  expect(second).not.toHaveBeenCalled();
  act(() => result.current.open("b"));
  unmount();
  expect(second.mock.calls).toEqual([["b"]]);
});

it("does not flush on read callback changes or after an explicit unread action", () => {
  const first = mock(), second = mock();
  const { result, rerender, unmount } = renderHook(({ markRead }) => useDeferredArticleRead({ scope: "feed", viewerKey: "alice:1", markRead }), { initialProps: { markRead: first } });
  act(() => result.current.open("a"));
  rerender({ markRead: second });
  expect(first).not.toHaveBeenCalled();
  act(() => result.current.cancel("a"));
  unmount();
  expect(first).not.toHaveBeenCalled();
  expect(second).not.toHaveBeenCalled();
});

it.each(["bob:1", "alice:2"])("discards pending marks across viewer/session changes (%s)", viewerKey => {
  const markRead = mock();
  const { result, rerender, unmount } = renderHook(({ viewerKey }) => useDeferredArticleRead({ scope: "feed", viewerKey, markRead }), { initialProps: { viewerKey: "alice:1" } });
  act(() => result.current.open("a"));
  rerender({ viewerKey });
  unmount();
  expect(markRead).not.toHaveBeenCalled();
});

it("does not mark during StrictMode setup, and flushes only once on departure", () => {
  const markRead = mock();
  const { result, unmount } = renderHook(() => useDeferredArticleRead({ scope: "feed", viewerKey: "alice:1", markRead }), { wrapper: ({ children }: { children: ReactNode }) => <StrictMode>{children}</StrictMode> });
  act(() => result.current.open("a"));
  expect(markRead).not.toHaveBeenCalled();
  unmount();
  expect(markRead.mock.calls).toEqual([["a"]]);
});

it("marks on actual page departure and does not duplicate the mark on unmount", () => {
  const markRead = mock();
  const { result, unmount } = renderHook(() => useDeferredArticleRead({ scope: "feed", viewerKey: "alice:1", markRead }));
  act(() => result.current.open("a"));
  act(() => window.dispatchEvent(new window.Event("pagehide")));
  unmount();
  expect(markRead.mock.calls).toEqual([["a"]]);
});

it("fences an authorization change that happens before the next render", () => {
  const markRead = mock();
  let current = true;
  const { result, unmount } = renderHook(() => useDeferredArticleRead({ scope: "feed", viewerKey: "alice:1", markRead }));
  act(() => result.current.open("a", () => current));
  current = false;
  unmount();
  expect(markRead).not.toHaveBeenCalled();
});

it("keeps same-card and owned portal actions selected until an outside click", async () => {
  const markRead = mock();
  const { result } = renderHook(() => useDeferredArticleRead({ scope: "feed", viewerKey: "alice:1", markRead }));
  const card = fixture("data-entry-id", "a");
  const popup = fixture("data-entry-owner", "a");
  const outside = fixture();
  try {
    act(() => result.current.open("a"));
    await act(async () => { fireEvent.click(card); fireEvent.click(popup); });
    expect(result.current.selectedEntryId).toBe("a");
    expect(markRead).not.toHaveBeenCalled();
    outside.addEventListener("click", event => event.stopPropagation());
    await act(async () => fireEvent.click(outside));
    expect(result.current.selectedEntryId).toBeNull();
    expect(markRead.mock.calls).toEqual([["a"]]);
    await act(async () => fireEvent.click(outside));
    expect(markRead).toHaveBeenCalledTimes(1);
  } finally { card.remove(); popup.remove(); outside.remove(); }
});

it("marks the previous article and leaves the newly clicked card highlighted and unread", async () => {
  const markRead = mock();
  const { result } = renderHook(() => useDeferredArticleRead({ scope: "feed", viewerKey: "alice:1", markRead }));
  const other = fixture("data-entry-id", "b");
  other.addEventListener("click", () => result.current.open("b"));
  try {
    act(() => result.current.open("a"));
    await act(async () => fireEvent.click(other));
    expect(markRead.mock.calls).toEqual([["a"]]);
    expect(result.current.selectedEntryId).toBe("b");
  } finally { other.remove(); }
});

it("treats another card's popup as outside and preserves an explicit unread decision", async () => {
  const markRead = mock();
  const { result } = renderHook(() => useDeferredArticleRead({ scope: "feed", viewerKey: "alice:1", markRead }));
  const popup = fixture("data-entry-owner", "b");
  try {
    act(() => result.current.open("a"));
    act(() => result.current.cancel("a"));
    expect(result.current.selectedEntryId).toBe("a");
    await act(async () => fireEvent.click(popup));
    expect(result.current.selectedEntryId).toBeNull();
    expect(markRead).not.toHaveBeenCalled();
  } finally { popup.remove(); }
});
