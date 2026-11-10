import { afterEach, expect, it, mock } from "bun:test";
import { act, cleanup, renderHook } from "@testing-library/react";
import { StrictMode, type ReactNode } from "react";
import { useDeferredArticleRead } from "@/hooks/useDeferredArticleRead";

afterEach(cleanup);

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
