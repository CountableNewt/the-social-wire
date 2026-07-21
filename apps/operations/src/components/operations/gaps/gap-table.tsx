import { DataColumnHeaders } from "@/components/operations/data-column-headers"
import { GapRow } from "@/components/operations/gaps/gap-row"
import { Table, TableBody, TableCell, TableHeader, TableRow } from "@/components/ui/table"
import type { Gap } from "@/lib/operations-types"

export function GapTable({
  gaps,
  onSelect,
  onInvestigate,
  allowBackfill,
  emptyMessage,
}: {
  gaps: Gap[]
  onSelect: (gap: Gap) => void
  onInvestigate: (gap: Gap) => void
  allowBackfill: boolean
  emptyMessage: string
}) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <DataColumnHeaders
            labels={["Status", "Cursor / Time Range (μs)", "Reason", "Detected", "Affected Collections", "Action"]}
          />
        </TableRow>
      </TableHeader>
      <TableBody>
        {gaps.length === 0 ? (
          <TableRow>
            <TableCell colSpan={6} className="py-8 text-center text-muted-foreground">
              {emptyMessage}
            </TableCell>
          </TableRow>
        ) : (
          gaps.map((gap) => (
            <GapRow
              key={gap.id}
              gap={gap}
              onSelect={onSelect}
              onInvestigate={onInvestigate}
              allowBackfill={allowBackfill}
            />
          ))
        )}
      </TableBody>
    </Table>
  )
}
