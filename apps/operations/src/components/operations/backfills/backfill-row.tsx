import { BackfillStatusIndicator } from "@/components/operations/backfills/backfill-status-indicator"
import { OperatorActionDialog } from "@/components/operations/operator-action-dialog"
import { Badge } from "@/components/ui/badge"
import { Progress } from "@/components/ui/progress"
import { TableCell, TableRow } from "@/components/ui/table"
import type { Backfill, EnvironmentName } from "@/lib/operations-types"

export function BackfillRow({ job, environment }: { job: Backfill; environment: EnvironmentName }) {
  const progress =
    job.estimatedCount > 0
      ? Math.round((job.processedCount / job.estimatedCount) * 100)
      : job.status === "completed"
        ? 100
        : 0
  const action = job.status === "running" ? "pause" : job.status === "paused" ? "resume" : undefined
  return (
    <TableRow>
      <TableCell className="font-mono">{job.id}</TableCell>
      <TableCell>
        <BackfillStatusIndicator status={job.status} />
      </TableCell>
      <TableCell className="font-mono">{job.collections[0] ?? "PDS reconciliation"}</TableCell>
      <TableCell className="font-mono">
        {job.startCursor ?? "—"} ..
        <br />
        {job.endCursor ?? "—"}
      </TableCell>
      <TableCell>
        <span className="font-mono">{progress}%</span>
        <Progress value={progress} className="mt-1 w-20" />
      </TableCell>
      <TableCell className="font-mono">{job.processedCount.toLocaleString()}</TableCell>
      <TableCell>{job.status === "running" ? `${job.rateLimit} rps` : "—"}</TableCell>
      <TableCell className="font-mono">{job.checkpointCursor ?? "—"}</TableCell>
      <TableCell>
        {action ? (
          <OperatorActionDialog
            environment={environment}
            path={`/v1/operations/backfills/${job.id}/${action}`}
            label={action === "pause" ? "Pause" : "Resume"}
          />
        ) : (
          <Badge>{job.status}</Badge>
        )}
      </TableCell>
    </TableRow>
  )
}
