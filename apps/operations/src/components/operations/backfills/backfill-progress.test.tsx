import { afterEach, describe, expect, it } from "bun:test"
import { cleanup, render, screen } from "@testing-library/react"
import { BackfillProgress, isBackfillTerminal } from "@/components/operations/backfills/backfill-progress"
import type { Backfill } from "@/lib/operations-types"

afterEach(cleanup)

const job: Backfill = {
  id: "bf-test-001",
  gapId: "gap-test-001",
  sourceMode: "jetstream_replay",
  status: "running",
  startCursor: 100,
  endCursor: 200,
  checkpointCursor: 150,
  collections: ["site.standard.document"],
  authorDids: [],
  batchSize: 1000,
  rateLimit: 500,
  maxConcurrency: 4,
  estimatedCount: 1000,
  processedCount: 500,
  failedCount: 2,
  reconciledCount: 40,
  requestedByDid: "did:plc:operator",
  auditNote: "Recover the confirmed gap",
  leaseOwner: "worker-01",
  leaseExpiresAt: "2026-07-20T20:01:00.000Z",
  createdAt: "2026-07-20T20:00:00.000Z",
  updatedAt: "2026-07-20T20:00:30.000Z",
}

describe("BackfillProgress", () => {
  it("shows live status, checkpoint, and progress metrics", () => {
    render(<BackfillProgress job={job} refreshing />)

    expect(screen.getByText("Running")).toBeTruthy()
    expect(screen.getByText("50%")).toBeTruthy()
    expect(screen.getByText("500 / 1,000")).toBeTruthy()
    expect(screen.getByText("Live Updates Every 2 Seconds")).toBeTruthy()
    expect(screen.getByText("worker-01")).toBeTruthy()
    expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe("50")
  })

  it("stops describing terminal jobs as live", () => {
    render(<BackfillProgress job={{ ...job, status: "completed", processedCount: 0 }} refreshing={false} />)

    expect(screen.getByText("Final Status")).toBeTruthy()
    expect(screen.getByText("100%")).toBeTruthy()
    expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe("100")
    expect(isBackfillTerminal("completed")).toBe(true)
    expect(isBackfillTerminal("running")).toBe(false)
  })

  it("warns when a queued job has not been claimed", () => {
    render(
      <BackfillProgress
        job={{ ...job, status: "queued", processedCount: 0, createdAt: "2026-07-20T19:00:00.000Z" }}
        refreshing={false}
      />,
    )

    expect(screen.getByText("Waiting for Worker")).toBeTruthy()
    expect(
      screen.getByText(
        "This job is queued but has not been claimed. Check the AppView Worker and its Operations database configuration if this state persists.",
      ),
    ).toBeTruthy()
  })
})
