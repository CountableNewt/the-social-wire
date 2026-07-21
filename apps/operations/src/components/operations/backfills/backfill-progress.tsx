import { RefreshCw } from "lucide-react"
import { BackfillStatusIndicator } from "@/components/operations/backfills/backfill-status-indicator"
import { BackfillDetail } from "@/components/operations/backfills/backfill-detail"
import { BackfillMetric } from "@/components/operations/backfills/backfill-metric"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Progress } from "@/components/ui/progress"
import { backfillProgressPercent } from "@/lib/backfill-progress"
import type { Backfill } from "@/lib/operations-types"

const terminalStatuses = new Set<Backfill["status"]>(["completed", "failed", "cancelled"])

export function isBackfillTerminal(status: Backfill["status"]) {
  return terminalStatuses.has(status)
}

export function BackfillProgress({ job, refreshing }: { job: Backfill; refreshing: boolean }) {
  const exactProgress = backfillProgressPercent(job)
  const progressLabel = exactProgress > 0 && exactProgress < 1 ? "<1%" : `${Math.round(exactProgress)}%`
  const active = !isBackfillTerminal(job.status)
  const waitingForWorker = job.status === "queued"
  return (
    <div className="flex-1 overflow-y-auto overscroll-contain p-4">
      <div aria-live="polite" className="rounded-md border bg-muted/20 p-3">
        <div className="flex items-center justify-between gap-3">
          <div>
            <p className="text-[10px] text-muted-foreground">Current Status</p>
            <div className="mt-1">
              <BackfillStatusIndicator status={job.status} />
            </div>
          </div>
          <div className="text-right text-[9px] text-muted-foreground">
            <p className="flex items-center justify-end gap-1">
              {active ? (
                <span className={refreshing ? "animate-spin" : undefined}>
                  <RefreshCw className="size-3" />
                </span>
              ) : null}
              {active ? "Live Updates Every 2 Seconds" : "Final Status"}
            </p>
            <p className="mt-1">Updated {new Date(job.updatedAt).toLocaleString()}</p>
          </div>
        </div>
        <div className="mt-4 flex items-end justify-between">
          <span className="font-mono text-xl font-semibold">{progressLabel}</span>
          <span className="font-mono text-[10px] text-muted-foreground">
            {job.processedCount.toLocaleString()} / {job.estimatedCount.toLocaleString()}
          </span>
        </div>
        <Progress value={exactProgress} className="mt-2 h-2" />
      </div>

      {waitingForWorker ? (
        <Alert variant="warning" className="mt-4">
          <AlertTitle>Waiting for Worker</AlertTitle>
          <AlertDescription>
            This job is queued but has not been claimed. Check the AppView Worker and its Operations database
            configuration if this state persists.
          </AlertDescription>
        </Alert>
      ) : null}

      <section className="mt-4">
        <h3 className="text-xs font-semibold">Backfill Progress</h3>
        <dl className="mt-2 grid grid-cols-2 gap-px overflow-hidden rounded-md border bg-border text-[10px]">
          <BackfillMetric label="Processed" value={job.processedCount.toLocaleString()} />
          <BackfillMetric
            label="Failed"
            value={job.failedCount.toLocaleString()}
            tone={job.failedCount > 0 ? "danger" : undefined}
          />
          <BackfillMetric label="Reconciled" value={job.reconciledCount.toLocaleString()} />
          <BackfillMetric label="Rate Limit" value={`${job.rateLimit.toLocaleString()} rps`} />
        </dl>
      </section>

      <section className="mt-4">
        <h3 className="text-xs font-semibold">Checkpoint</h3>
        <dl className="mt-2 divide-y rounded-md border text-[10px]">
          <BackfillDetail label="Cursor" value={job.checkpointCursor?.toLocaleString() ?? "Waiting for worker"} mono />
          <BackfillDetail label="Worker" value={job.leaseOwner ?? "Not claimed yet"} mono />
          <BackfillDetail
            label="Lease Expires"
            value={job.leaseExpiresAt ? new Date(job.leaseExpiresAt).toLocaleString() : "—"}
          />
        </dl>
      </section>

      <section className="mt-4">
        <h3 className="text-xs font-semibold">Queued Request</h3>
        <dl className="mt-2 divide-y rounded-md border text-[10px]">
          <BackfillDetail label="Job ID" value={job.id} mono />
          <BackfillDetail label="Range (μs)" value={`${job.startCursor ?? "—"} .. ${job.endCursor ?? "—"}`} mono />
          <BackfillDetail label="Collections" value={job.collections.join(", ") || "PDS reconciliation"} mono />
          <BackfillDetail label="Requested" value={new Date(job.createdAt).toLocaleString()} />
        </dl>
      </section>
    </div>
  )
}
