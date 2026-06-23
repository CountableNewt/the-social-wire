import { describe, expect, it } from "bun:test";

import {
  shouldFillViewportFetch,
  shouldScrollNearEndFetch,
} from "@/lib/entryListPaginationTriggers";

describe("entryListPaginationTriggers", () => {
  it("requests fill when content does not cover the viewport", () => {
    expect(
      shouldFillViewportFetch({
        scrollHeight: 400,
        clientHeight: 800,
        hasNextPage: true,
        isFetchingNextPage: false,
      })
    ).toBe(true);
  });

  it("does not fill-fetch when already fetching or exhausted", () => {
    expect(
      shouldFillViewportFetch({
        scrollHeight: 400,
        clientHeight: 800,
        hasNextPage: false,
        isFetchingNextPage: false,
      })
    ).toBe(false);
    expect(
      shouldFillViewportFetch({
        scrollHeight: 400,
        clientHeight: 800,
        hasNextPage: true,
        isFetchingNextPage: true,
      })
    ).toBe(false);
  });

  it("detects near-end scroll position", () => {
    expect(
      shouldScrollNearEndFetch({
        scrollTop: 700,
        scrollHeight: 1000,
        clientHeight: 250,
        threshold: 100,
      })
    ).toBe(true);
    expect(
      shouldScrollNearEndFetch({
        scrollTop: 100,
        scrollHeight: 1000,
        clientHeight: 250,
        threshold: 100,
      })
    ).toBe(false);
  });
});
