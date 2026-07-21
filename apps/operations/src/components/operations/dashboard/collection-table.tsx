import { DataColumnHeaders } from "@/components/operations/data-column-headers"
import { MetricSparklineCell } from "@/components/operations/metric-sparkline-cell"
import { OperationsSection } from "@/components/operations/operations-section"
import { Table, TableBody, TableCell, TableHeader, TableRow } from "@/components/ui/table"
import { collectionMetricRows } from "@/lib/collection-metrics"
import type { MetricRollup } from "@/lib/operations-types"

const formatRate = (value: number) =>
  value < 1 ? value.toLocaleString(undefined, { maximumFractionDigits: 2 }) : Math.round(value).toLocaleString()
const formatMilliseconds = (value: number) => `${Math.round(value).toLocaleString()} ms`

export function CollectionTable({ metricRollups }: { metricRollups: MetricRollup[] }) {
  const rows = collectionMetricRows(metricRollups)

  return (
    <OperationsSection title="Events / sec by Bounded Collection / Operation (15 minutes)">
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
                "Avg Commit Time (ms)",
                "Max Commit Time (ms)",
                "Errors (eps)",
              ]}
            />
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.length === 0 ? (
            <TableRow>
              <TableCell colSpan={8} className="py-8 text-center text-muted-foreground">
                No collection metric history is available for the last 15 minutes.
              </TableCell>
            </TableRow>
          ) : (
            rows.map((row) => (
              <TableRow key={row.collection}>
                <TableCell className="font-mono">{row.collection}</TableCell>
                <TableCell>
                  <MetricSparklineCell points={row.createRate} label={`${row.collection} create rate`} format={formatRate} />
                </TableCell>
                <TableCell>
                  <MetricSparklineCell points={row.updateRate} label={`${row.collection} update rate`} format={formatRate} />
                </TableCell>
                <TableCell>
                  <MetricSparklineCell points={row.deleteRate} label={`${row.collection} delete rate`} format={formatRate} />
                </TableCell>
                <TableCell>
                  <MetricSparklineCell
                    points={row.allOperationsRate}
                    label={`${row.collection} all operation rate`}
                    format={formatRate}
                  />
                </TableCell>
                <TableCell>
                  <MetricSparklineCell
                    points={row.averageCommitMilliseconds}
                    label={`${row.collection} average commit time`}
                    format={formatMilliseconds}
                  />
                </TableCell>
                <TableCell>
                  <MetricSparklineCell
                    points={row.maximumCommitMilliseconds}
                    label={`${row.collection} maximum commit time`}
                    format={formatMilliseconds}
                  />
                </TableCell>
                <TableCell>
                  <MetricSparklineCell
                    points={row.failedRate}
                    label={`${row.collection} error rate`}
                    format={formatRate}
                    tone="warning"
                  />
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </OperationsSection>
  )
}
