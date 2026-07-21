import type { Backfill } from "@/lib/operations-types"

export function backfillProgressPercent(job: Backfill): number {
  if (job.status === "completed") return 100
  if (job.estimatedCount <= 0) return 0

  return Math.min(100, Math.max(0, (job.processedCount / job.estimatedCount) * 100))
}
