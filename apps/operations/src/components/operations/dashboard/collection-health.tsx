import { DataColumnHeaders } from "@/components/operations/data-column-headers"
import { collectionRows } from "@/components/operations/dashboard/operations-demo-metrics"
import { OperationsSection } from "@/components/operations/operations-section"
import { Sparkline } from "@/components/operations/sparkline"
import { Table, TableBody, TableCell, TableHeader, TableRow } from "@/components/ui/table"

export function CollectionHealth() {
  return (
    <OperationsSection title="Collection Health (per bounded collection)">
      <Table>
        <TableHeader>
          <TableRow>
            <DataColumnHeaders
              labels={[
                "Collection",
                "Accepted (eps)",
                "Filtered (eps)",
                "Failed (eps)",
                "p95 Commit Time (ms)",
                "Newest-Event Age",
                "Lag (s)",
                "Status",
              ]}
            />
          </TableRow>
        </TableHeader>
        <TableBody>
          {collectionRows.map((row, index) => (
            <TableRow key={row[0]}>
              <TableCell className="font-mono">{row[0]}</TableCell>
              <TableCell>
                {row[4].toLocaleString()} <Sparkline />
              </TableCell>
              <TableCell>
                {12 + index * 8} <Sparkline />
              </TableCell>
              <TableCell>
                {row[8]} <Sparkline tone={index < 2 ? "warning" : "primary"} />
              </TableCell>
              <TableCell>{row[7].toLocaleString()}</TableCell>
              <TableCell>{(2.1 - index * 0.5).toFixed(1)}s</TableCell>
              <TableCell>{(2.1 - index * 0.5).toFixed(1)}</TableCell>
              <TableCell>
                <span className={index < 2 ? "ops-warning" : "ops-success"}>{index < 2 ? "At Risk" : "Good"}</span>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </OperationsSection>
  )
}
