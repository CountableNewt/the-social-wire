import { afterEach, describe, expect, it, mock } from "bun:test"
import { cleanup, render, screen, within } from "@testing-library/react"
import { GapsTable } from "@/components/operations/gaps/gaps-table"
import type { Backfill, Gap } from "@/lib/operations-types"

afterEach(cleanup)

const activeGap: Gap = {
  id: "gap-active",
  source: "jetstream",
  reason: "consumer_restart",
  status: "confirmed",
  collections: ["site.standard.document"],
  detectedAt: "2026-07-20T20:00:00.000Z",
  updatedAt: "2026-07-20T20:00:00.000Z",
  discoveredCount: 10,
  processedCount: 0,
  failedCount: 0,
  reconciledCount: 0,
}

const backfilledGap: Gap = {
  ...activeGap,
  id: "gap-backfilled",
  status: "resolved",
  backfillJobId: "backfill-completed",
}

const completedBackfill: Backfill = {
  id: "backfill-completed",
  gapId: "gap-backfilled",
  sourceMode: "jetstream_replay",
  status: "completed",
  collections: ["site.standard.document"],
  authorDids: [],
  batchSize: 1000,
  rateLimit: 500,
  maxConcurrency: 4,
  estimatedCount: 10,
  processedCount: 10,
  failedCount: 0,
  reconciledCount: 0,
  requestedByDid: "did:plc:operator",
  auditNote: "Recover gap",
  createdAt: "2026-07-20T20:00:00.000Z",
  updatedAt: "2026-07-20T20:00:30.000Z",
  completedAt: "2026-07-20T20:00:30.000Z",
}

describe("GapsTable", () => {
  it("separates completed recoveries from active gaps", () => {
    render(
      <GapsTable
        gaps={[activeGap, backfilledGap]}
        backfills={[completedBackfill]}
        onSelect={mock()}
        onInvestigate={mock()}
        expanded
      />,
    )

    const activeSection = screen.getByRole("heading", { name: "Active Gaps (1)" }).closest("section")
    const backfilledSection = screen.getByRole("heading", { name: "Backfilled Gaps (1)" }).closest("section")

    expect(activeSection).not.toBeNull()
    expect(backfilledSection).not.toBeNull()
    expect(within(activeSection!).getByRole("button", { name: "Backfill" })).toBeTruthy()
    expect(within(backfilledSection!).queryByRole("button", { name: "Backfill" })).toBeNull()
    expect(within(backfilledSection!).getByText("resolved")).toBeTruthy()
  })
})
