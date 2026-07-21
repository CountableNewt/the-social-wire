import { describe, expect, it } from "bun:test"
import { backfillProgressPercent } from "@/lib/backfill-progress"
import type { Backfill } from "@/lib/operations-types"

const job: Backfill = {
  id: "bf-test-001",
  sourceMode: "jetstream_replay",
  status: "running",
  collections: ["site.standard.document"],
  authorDids: [],
  batchSize: 1000,
  rateLimit: 500,
  maxConcurrency: 4,
  estimatedCount: 1000,
  processedCount: 500,
  failedCount: 0,
  reconciledCount: 0,
  requestedByDid: "did:plc:operator",
  auditNote: "Recover gap",
  createdAt: "2026-07-20T20:00:00.000Z",
  updatedAt: "2026-07-20T20:00:30.000Z",
}

describe("backfillProgressPercent", () => {
  it("uses processed work while a job is active", () => {
    expect(backfillProgressPercent(job)).toBe(50)
  })

  it("treats a completed no-op backfill as fully complete", () => {
    expect(backfillProgressPercent({ ...job, status: "completed", processedCount: 0 })).toBe(100)
  })

  it("preserves partial progress for a failed job", () => {
    expect(backfillProgressPercent({ ...job, status: "failed", processedCount: 250 })).toBe(25)
  })
})
