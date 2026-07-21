import { ExternalLink } from "lucide-react"
import Link from "next/link"
import { DataColumnHeaders } from "@/components/operations/data-column-headers"
import { GapCollectionScope } from "@/components/operations/gaps/gap-collection-scope"
import { OperationsSection } from "@/components/operations/operations-section"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHeader, TableRow } from "@/components/ui/table"
import type { Gap } from "@/lib/operations-types"

export function GapsTable({
  gaps,
  onSelect,
  onInvestigate,
  expanded,
}: {
  gaps: Gap[]
  onSelect: (gap: Gap) => void
  onInvestigate: (gap: Gap) => void
  expanded?: boolean
}) {
  const action = expanded ? undefined : (
    <Link href="/gaps" className="text-[10px] text-primary">
      View All Gaps <ExternalLink className="inline size-3" />
    </Link>
  )

  return (
    <OperationsSection title={`Known Gaps (${gaps.length})`} action={action}>
      <Table>
        <TableHeader>
          <TableRow>
            <DataColumnHeaders
              labels={["Status", "Cursor / Time Range (μs)", "Reason", "Detected", "Affected Collections", "Action"]}
            />
          </TableRow>
        </TableHeader>
        <TableBody>
          {gaps.map((gap) => (
            <TableRow key={gap.id}>
              <TableCell>
                <Badge
                  tone={gap.status === "confirmed" ? "danger" : gap.status === "backfilling" ? "warning" : "neutral"}
                >
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
                  <Button size="sm" variant="outline" onClick={() => onSelect(gap)}>
                    Backfill
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </OperationsSection>
  )
}
