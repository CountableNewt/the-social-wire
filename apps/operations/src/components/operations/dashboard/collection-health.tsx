import { DataColumnHeaders } from "@/components/operations/data-column-headers"
import { MetricSparklineCell } from "@/components/operations/metric-sparkline-cell"
import { OperationsSection } from "@/components/operations/operations-section"
import { Table, TableBody, TableCell, TableHeader, TableRow } from "@/components/ui/table"
import { collectionMetricRows, currentMetricValue } from "@/lib/collection-metrics"
import type { MetricRollup } from "@/lib/operations-types"

const formatRate = (value: number) =>
  value < 1 ? value.toLocaleString(undefined, { maximumFractionDigits: 2 }) : Math.round(value).toLocaleString()
const formatMilliseconds = (value: number) => `${Math.round(value).toLocaleString()} ms`
const formatSeconds = (value: number) => `${value.toLocaleString(undefined, { maximumFractionDigits: 2 })} s`

export function CollectionHealth({ metricRollups, refreshedAt }: { metricRollups: MetricRollup[]; refreshedAt: string }) {
  const rows = collectionMetricRows(metricRollups, refreshedAt)

  return (
    <OperationsSection title="Collection Health (15 minutes)">
      <Table>
        <TableHeader>
          <TableRow>
            <DataColumnHeaders
              labels={[
                "Collection",
                "Accepted (eps)",
                "Failed (eps)",
                "Avg Commit Time (ms)",
                "Max Commit Time (ms)",
                "Avg Event Lag (s)",
                "Max Event Lag (s)",
                "Status",
              ]}
            />
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.length === 0 ? (
            <TableRow>
              <TableCell colSpan={8} className="py-8 text-center text-muted-foreground">
                No collection health history is available for the last 15 minutes.
              </TableCell>
            </TableRow>
          ) : (
            rows.map((row) => {
              const currentAccepted = currentMetricValue(row.acceptedRate)
              const currentFailed = currentMetricValue(row.failedRate)
              const status =
                currentAccepted === null && currentFailed === null ? "Unknown" : (currentFailed ?? 0) > 0 ? "At Risk" : "Good"
              return (
                <TableRow key={row.collection}>
                  <TableCell className="font-mono">{row.collection}</TableCell>
                  <TableCell>
                    <MetricSparklineCell
                      points={row.acceptedRate}
                      label={`${row.collection} accepted rate`}
                      format={formatRate}
                    />
                  </TableCell>
                  <TableCell>
                    <MetricSparklineCell
                      points={row.failedRate}
                      label={`${row.collection} failed rate`}
                      format={formatRate}
                      tone="warning"
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
                      points={row.averageLagSeconds}
                      label={`${row.collection} average event lag`}
                      format={formatSeconds}
                    />
                  </TableCell>
                  <TableCell>
                    <MetricSparklineCell
                      points={row.maximumLagSeconds}
                      label={`${row.collection} maximum event lag`}
                      format={formatSeconds}
                    />
                  </TableCell>
                  <TableCell>
                    <span
                      className={status === "At Risk" ? "ops-warning" : status === "Good" ? "ops-success" : "text-muted-foreground"}
                    >
                      {status}
                    </span>
                  </TableCell>
                </TableRow>
              )
            })
          )}
        </TableBody>
      </Table>
    </OperationsSection>
  )
}
