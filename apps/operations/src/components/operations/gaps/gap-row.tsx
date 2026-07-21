import { GapCollectionScope } from "@/components/operations/gaps/gap-collection-scope"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { TableCell, TableRow } from "@/components/ui/table"
import type { Gap } from "@/lib/operations-types"

export function GapRow({
  gap,
  onSelect,
  onInvestigate,
  allowBackfill,
}: {
  gap: Gap
  onSelect: (gap: Gap) => void
  onInvestigate: (gap: Gap) => void
  allowBackfill: boolean
}) {
  return (
    <TableRow>
      <TableCell>
        <Badge tone={gap.status === "confirmed" ? "danger" : gap.status === "backfilling" ? "warning" : "neutral"}>
          {gap.status}
        </Badge>
      </TableCell>
      <TableCell className="font-mono">
        {gap.startCursor ?? "—"} ..
        <br />
        {gap.endCursor ?? "—"}
      </TableCell>
      <TableCell>{gap.reason.replaceAll("_", " ")}</TableCell>
      <TableCell>{new Date(gap.detectedAt).toLocaleString()}</TableCell>
      <TableCell>
        <GapCollectionScope collections={gap.collections} />
      </TableCell>
      <TableCell>
        <div className="flex items-center gap-1.5">
          <Button size="sm" variant="outline" onClick={() => onInvestigate(gap)}>
            Investigate
          </Button>
          {allowBackfill ? (
            <Button size="sm" variant="outline" onClick={() => onSelect(gap)}>
              Backfill
            </Button>
          ) : null}
        </div>
      </TableCell>
    </TableRow>
  )
}
