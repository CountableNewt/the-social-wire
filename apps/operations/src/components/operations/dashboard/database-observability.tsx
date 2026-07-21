import { Database } from "lucide-react"
import { DataColumnHeaders } from "@/components/operations/data-column-headers"
import { Table, TableBody, TableCell, TableHeader, TableRow } from "@/components/ui/table"
import { boundedNonNegativeInteger, boundedNonNegativeNumber } from "@/lib/observability-values"
import type { Overview, Span } from "@/lib/operations-types"

const percentile = (values: number[], value: number) => {
  if (!values.length) return undefined
  const sorted = [...values].sort((left, right) => left - right)
  return sorted[Math.min(sorted.length - 1, Math.ceil(sorted.length * value) - 1)]
}

const formatBytes = (bytes?: number) => {
  const bounded = boundedNonNegativeNumber(bytes)
  if (bounded === null) return "—"
  if (bounded < 1_000_000_000) return `${(bounded / 1_000_000).toFixed(1)} MB`
  return `${(bounded / 1_000_000_000).toFixed(1)} GB`
}

const formatCount = (value?: number) => boundedNonNegativeInteger(value)?.toLocaleString() ?? "—"

const formatCacheHitRatio = (value?: number) => {
  const bounded = boundedNonNegativeNumber(value)
  return bounded !== null && bounded <= 1 ? `${(bounded * 100).toFixed(1)}%` : "—"
}

const isDatabaseSpan = (span: Span) =>
  Boolean(span.attributes.query_name) || span.name.includes(".db.") || span.name.includes("database")

export function DatabaseObservability({ overview }: { overview: Overview }) {
  const database = overview.database
  const spans = overview.recentTraces.filter(isDatabaseSpan)
  const observedDurations = spans
    .map((span) => boundedNonNegativeNumber(span.durationMs))
    .filter((value): value is number => value !== null)
  const p95 = percentile(observedDurations, 0.95)
  const slowQueries = observedDurations.filter((duration) => duration >= 1_000).length
  const dependencyStates = overview.services
    .flatMap((service) => [service.dependencyState.database, service.dependencyState.operations_database])
    .filter(Boolean)
  const databaseReady =
    dependencyStates.length > 0 && dependencyStates.every((state) => ["healthy", "ready"].includes(state))
  const metrics = [
    {
      label: "Database Availability",
      value: dependencyStates.length ? (databaseReady ? "Ready" : "Degraded") : "Unknown",
      note: `${dependencyStates.length} service dependencies reporting`,
    },
    { label: "Database Size", value: formatBytes(database?.databaseSizeBytes), note: "Current Postgres database size" },
    {
      label: "Database Request Volume",
      value: formatCount(database?.transactionsTotal),
      note: database?.statsResetAt
        ? `Transactions since ${new Date(database.statsResetAt).toLocaleDateString()}`
        : "Committed + rolled-back transactions",
    },
    {
      label: "Estimated Records",
      value: formatCount(database?.estimatedRecords),
      note: "Live-row estimates across user tables",
    },
    {
      label: "Connections",
      value: database
        ? `${formatCount(database.activeConnections)} / ${formatCount(database.maxConnections)}`
        : "—",
      note: "Active connections / configured maximum",
    },
    {
      label: "Cache Hit Ratio",
      value: formatCacheHitRatio(database?.cacheHitRatio),
      note: "Postgres shared-buffer hit ratio",
    },
  ]

  return (
    <section className="ops-panel min-w-0 overflow-hidden" aria-label="Database Observability">
      <header className="flex min-h-9 items-center justify-between gap-3 border-b px-3">
        <h2 className="flex items-center gap-2 text-xs font-semibold">
          <Database className="size-3.5" /> Database Observability
        </h2>
        <span className="text-right text-[9px] text-muted-foreground">
          Sampled DB span p95 {p95 === undefined ? "—" : `${p95.toLocaleString()} ms`} · n={observedDurations.length} ·{" "}
          {slowQueries} slow
        </span>
      </header>
      <div className="grid divide-y sm:grid-cols-2 sm:divide-x xl:grid-cols-3">
        {metrics.map((metric) => (
          <div key={metric.label} className="p-3">
            <p className="text-[10px] text-muted-foreground">{metric.label}</p>
            <p className="mt-1 font-mono text-sm font-medium">{metric.value}</p>
            <p className="mt-1 text-[9px] text-muted-foreground">{metric.note}</p>
          </div>
        ))}
      </div>
      {database?.topTables.length ? (
        <div className="border-t">
          <h3 className="px-3 py-2 text-[10px] font-semibold">Top Tables by Estimated Records</h3>
          <Table>
            <TableHeader>
              <TableRow>
                <DataColumnHeaders labels={["Schema", "Table", "Estimated Records"]} />
              </TableRow>
            </TableHeader>
            <TableBody>
              {database.topTables.slice(0, 5).map((table) => (
                <TableRow key={`${table.schema}.${table.table}`}>
                  <TableCell>{table.schema}</TableCell>
                  <TableCell className="font-mono">{table.table}</TableCell>
                  <TableCell className="font-mono">{formatCount(table.estimatedRecords)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : null}
      <div className="border-t">
        <h3 className="px-3 py-2 text-[10px] font-semibold">Recent Correlated Database Spans</h3>
        {spans.length ? (
          <Table>
            <TableHeader>
              <TableRow>
                <DataColumnHeaders labels={["Time", "Service", "Query", "Duration", "Status", "Trace ID"]} />
              </TableRow>
            </TableHeader>
            <TableBody>
              {spans.slice(0, 5).map((span) => (
                <TableRow key={span.id}>
                  <TableCell className="font-mono">{new Date(span.startedAt).toLocaleTimeString()}</TableCell>
                  <TableCell>{span.service}</TableCell>
                  <TableCell className="font-mono">{span.attributes.query_name ?? span.name}</TableCell>
                  <TableCell className="font-mono">{span.durationMs.toLocaleString()} ms</TableCell>
                  <TableCell className={span.status === "error" ? "text-destructive" : "ops-success"}>
                    {span.status}
                  </TableCell>
                  <TableCell className="font-mono">{span.traceId.slice(0, 16)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <p className="p-3 text-[10px] text-muted-foreground">
            No correlated database spans were reported in this time range.
          </p>
        )}
      </div>
    </section>
  )
}
