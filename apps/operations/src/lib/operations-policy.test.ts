import { describe, expect, test } from "bun:test"
import { canQueueBackfill, filterTraces, productionConfirmationMatches } from "@/lib/operations-policy"
import type { Span } from "@/lib/operations-types"

describe("operations mutation safeguards", () => {
  test("requires the exact production environment confirmation", () => {
    expect(productionConfirmationMatches("production", "production")).toBe(false)
    expect(productionConfirmationMatches("production", "PRODUCTION")).toBe(true)
    expect(productionConfirmationMatches("development", "")).toBe(true)
  })

  test("requires dry-run, review, audit note, and idle mutation", () => {
    const ready = { dryRunComplete: true, reviewed: true, environment: "development" as const, environmentConfirmation: "", auditNote: "Recover confirmed gap", pending: false }
    expect(canQueueBackfill(ready)).toBe(true)
    expect(canQueueBackfill({ ...ready, dryRunComplete: false })).toBe(false)
    expect(canQueueBackfill({ ...ready, auditNote: "short" })).toBe(false)
    expect(canQueueBackfill({ ...ready, pending: true })).toBe(false)
  })
})

test("trace filtering searches bounded attributes", () => {
  const spans = [{ id: "1", traceId: "abc", service: "appview", name: "appview.db.query", startedAt: "2026-01-01T00:00:00Z", durationMs: 10, status: "ok", attributes: { query_name: "sidebar" }, expiresAt: "2026-01-02T00:00:00Z" }] satisfies Span[]
  expect(filterTraces(spans, "sidebar")).toHaveLength(1)
  expect(filterTraces(spans, "raw-did")).toHaveLength(0)
})
