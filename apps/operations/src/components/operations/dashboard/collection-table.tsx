import { DataColumnHeaders } from "@/components/operations/data-column-headers"
import { collectionRows } from "@/components/operations/dashboard/operations-demo-metrics"
import { OperationsSection } from "@/components/operations/operations-section"
import { Sparkline } from "@/components/operations/sparkline"
import { Table, TableBody, TableCell, TableHeader, TableRow } from "@/components/ui/table"

export function CollectionTable() {
  return (
    <OperationsSection title="Events / sec by Bounded Collection / Operation (top)">
      <Table>
        <TableHeader>
          <TableRow>
            <DataColumnHeaders
              labels={[
                "Collection",
                "Create (eps)",
                "Update (eps)",
                "Delete (eps)",
                "All Ops (eps)",
                "In-Flight",
                "p50 (ms) R→C",
                "p95 (ms) R→C",
                "Errors (eps)",
              ]}
            />
          </TableRow>
        </TableHeader>
        <TableBody>
          {collectionRows.map((row) => (
            <TableRow key={row[0]}>
              {row.map((cell, index) => (
                <TableCell key={index} className={index === 0 ? "font-mono" : "font-mono tabular-nums"}>
                  {cell}
                  {index > 0 && index < 5 ? <Sparkline /> : null}
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </OperationsSection>
  )
}
