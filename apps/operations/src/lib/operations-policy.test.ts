import { describe, expect, test } from "bun:test"
import {
  backfillReadiness,
  canQueueBackfill,
  filterTraces,
  productionConfirmationMatches,
} from "@/lib/operations-policy"
import type { Span } from "@/lib/operations-types"

describe("operations mutation safeguards", () => {
  test("requires the exact production environment confirmation", () => {
    expect(productionConfirmationMatches("production", "production")).toBe(false)
    expect(productionConfirmationMatches("production", "PRODUCTION")).toBe(true)
    expect(productionConfirmationMatches("development", "")).toBe(true)
  })

  test("requires collection scope, dry-run, review, and an idle mutation", () => {
    const ready = {
      collectionScopeSelected: true,
      dryRunComplete: true,
      reviewed: true,
      environment: "development" as const,
      environmentConfirmation: "",
      pending: false,
    }
    expect(canQueueBackfill(ready)).toBe(true)
    expect(canQueueBackfill({ ...ready, collectionScopeSelected: false })).toBe(false)
    expect(canQueueBackfill({ ...ready, dryRunComplete: false })).toBe(false)
    expect(canQueueBackfill({ ...ready, reviewed: false })).toBe(false)
    expect(canQueueBackfill({ ...ready, pending: true })).toBe(false)
  })

  test("reports the exact unmet backfill requirements", () => {
    const requirements = backfillReadiness({
      collectionScopeSelected: true,
      dryRunComplete: true,
      reviewed: false,
      environment: "production",
      environmentConfirmation: "production",
      pending: false,
    })
    expect(requirements.filter((requirement) => !requirement.complete).map((requirement) => requirement.id)).toEqual([
      "reviewed",
      "production-confirmation",
    ])
  })
})

test("trace filtering searches bounded attributes", () => {
  const spans = [
    {
      id: "1",
      traceId: "abc",
      service: "appview",
      name: "appview.db.query",
      startedAt: "2026-01-01T00:00:00Z",
      durationMs: 10,
      status: "ok",
      attributes: { query_name: "sidebar" },
      expiresAt: "2026-01-02T00:00:00Z",
    },
  ] satisfies Span[]
  expect(filterTraces(spans, "sidebar")).toHaveLength(1)
  expect(filterTraces(spans, "raw-did")).toHaveLength(0)
})
