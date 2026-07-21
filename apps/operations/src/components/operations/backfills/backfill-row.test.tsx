import { afterEach, describe, expect, it } from "bun:test"
import { cleanup, render, screen } from "@testing-library/react"
import { BackfillRow } from "@/components/operations/backfills/backfill-row"
import { Table, TableBody } from "@/components/ui/table"
import type { Backfill } from "@/lib/operations-types"

afterEach(cleanup)

const completedJob: Backfill = {
  id: "bf-completed-empty",
  sourceMode: "jetstream_replay",
  status: "completed",
  collections: ["site.standard.document"],
  authorDids: [],
  batchSize: 1000,
  rateLimit: 500,
  maxConcurrency: 4,
  estimatedCount: 1000,
  processedCount: 0,
  failedCount: 0,
  reconciledCount: 0,
  requestedByDid: "did:plc:operator",
  auditNote: "Recover gap",
  createdAt: "2026-07-20T20:00:00.000Z",
  updatedAt: "2026-07-20T20:00:30.000Z",
}

describe("BackfillRow", () => {
  it("shows completed jobs at 100 percent even when no records needed processing", () => {
    render(
      <Table>
        <TableBody>
          <BackfillRow job={completedJob} environment="development" />
        </TableBody>
      </Table>,
    )

    expect(screen.getByText("100%")).toBeTruthy()
    expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe("100")
  })

  it("shows the persisted failure reason for failed jobs", () => {
    render(
      <Table>
        <TableBody>
          <BackfillRow
            job={{ ...completedJob, status: "failed", failureReason: "database_timeout" }}
            environment="development"
          />
        </TableBody>
      </Table>,
    )

    expect(screen.getByRole("alert").textContent).toContain("Failure Reason: database timeout")
  })
})
