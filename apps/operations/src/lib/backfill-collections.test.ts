import { describe, expect, it } from "bun:test"
import { gapCollectionScopeLabel, initialBackfillCollections } from "@/lib/backfill-collections"

describe("backfill collection scope", () => {
  it("does not substitute a default collection for an unknown scope", () => {
    expect(initialBackfillCollections([])).toEqual([])
    expect(gapCollectionScopeLabel([])).toBe("Unknown")
  })
})
