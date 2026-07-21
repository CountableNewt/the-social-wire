import type { MetricRollup } from "@/lib/operations-types"

const EVENTS_TOTAL = "socialwire.ingestion.events_total"
const RESULTS_TOTAL = "socialwire.ingestion.results_total"
const COMMIT_DURATION = "socialwire.ingestion.db_write_duration_seconds"
const COMMIT_LAG = "socialwire.ingestion.commit_lag_seconds"

export type MetricPoint = { timestamp: number; value: number | null }

export type CollectionMetricRow = {
  collection: string
  createRate: MetricPoint[]
  updateRate: MetricPoint[]
  deleteRate: MetricPoint[]
  allOperationsRate: MetricPoint[]
  acceptedRate: MetricPoint[]
  failedRate: MetricPoint[]
  averageCommitMilliseconds: MetricPoint[]
  maximumCommitMilliseconds: MetricPoint[]
  averageLagSeconds: MetricPoint[]
  maximumLagSeconds: MetricPoint[]
}

type Aggregate = { sum: number; count: number; maximum: number | null }
type Series = Map<number, Aggregate>
type MutableCollectionMetricRow = { [Key in keyof Omit<CollectionMetricRow, "collection">]: Series } & {
  collection: string
}

function emptySeries(): Series {
  return new Map()
}

function emptyRow(collection: string): MutableCollectionMetricRow {
  return {
    collection,
    createRate: emptySeries(),
    updateRate: emptySeries(),
    deleteRate: emptySeries(),
    allOperationsRate: emptySeries(),
    acceptedRate: emptySeries(),
    failedRate: emptySeries(),
    averageCommitMilliseconds: emptySeries(),
    maximumCommitMilliseconds: emptySeries(),
    averageLagSeconds: emptySeries(),
    maximumLagSeconds: emptySeries(),
  }
}

function append(series: Series, timestamp: number, rollup: MetricRollup) {
  const current = series.get(timestamp) ?? { sum: 0, count: 0, maximum: null }
  current.sum += rollup.valueSum
  current.count += rollup.sampleCount
  if (rollup.valueMax !== undefined) current.maximum = Math.max(current.maximum ?? -Infinity, rollup.valueMax)
  series.set(timestamp, current)
}

function points(series: Series, value: (aggregate: Aggregate) => number): MetricPoint[] {
  return [...series.entries()]
    .sort(([left], [right]) => left - right)
    .map(([timestamp, aggregate]) => ({ timestamp, value: value(aggregate) }))
}

function counterRate(series: Series, timestamps: number[]) {
  return timestamps.map((timestamp) => ({ timestamp, value: (series.get(timestamp)?.sum ?? 0) / 60 }))
}

function average(series: Series, multiplier = 1) {
  return points(series, ({ sum, count }) => (count > 0 ? (sum / count) * multiplier : 0))
}

function maximum(series: Series, multiplier = 1) {
  return points(series, ({ maximum: value }) => (value ?? 0) * multiplier)
}

export function collectionMetricRows(rollups: MetricRollup[]): CollectionMetricRow[] {
  const rows = new Map<string, MutableCollectionMetricRow>()

  for (const rollup of rollups) {
    const collection = rollup.dimensions.collection
    if (!collection || (rollup.dimensions.ingestion_mode && rollup.dimensions.ingestion_mode !== "live")) continue

    const timestamp = new Date(rollup.bucketStart).getTime()
    if (!Number.isFinite(timestamp)) continue
    const row = rows.get(collection) ?? emptyRow(collection)
    rows.set(collection, row)

    if (rollup.metricName === EVENTS_TOTAL) {
      append(row.allOperationsRate, timestamp, rollup)
      if (rollup.dimensions.operation === "create") append(row.createRate, timestamp, rollup)
      if (rollup.dimensions.operation === "update") append(row.updateRate, timestamp, rollup)
      if (rollup.dimensions.operation === "delete") append(row.deleteRate, timestamp, rollup)
    }

    if (rollup.metricName === RESULTS_TOTAL && rollup.dimensions.result === "success") {
      append(row.acceptedRate, timestamp, rollup)
    }
    if (rollup.metricName === RESULTS_TOTAL && rollup.dimensions.result === "error") {
      append(row.failedRate, timestamp, rollup)
    }
    if (rollup.metricName === COMMIT_DURATION) {
      append(row.averageCommitMilliseconds, timestamp, rollup)
      append(row.maximumCommitMilliseconds, timestamp, rollup)
    }
    if (rollup.metricName === COMMIT_LAG) {
      append(row.averageLagSeconds, timestamp, rollup)
      append(row.maximumLagSeconds, timestamp, rollup)
    }
  }

  return [...rows.values()]
    .map((row) => {
      const timestamps = [...row.allOperationsRate.keys()].sort((left, right) => left - right)
      return {
        collection: row.collection,
        createRate: counterRate(row.createRate, timestamps),
        updateRate: counterRate(row.updateRate, timestamps),
        deleteRate: counterRate(row.deleteRate, timestamps),
        allOperationsRate: counterRate(row.allOperationsRate, timestamps),
        acceptedRate: counterRate(row.acceptedRate, timestamps),
        failedRate: counterRate(row.failedRate, timestamps),
        averageCommitMilliseconds: average(row.averageCommitMilliseconds, 1_000),
        maximumCommitMilliseconds: maximum(row.maximumCommitMilliseconds, 1_000),
        averageLagSeconds: average(row.averageLagSeconds),
        maximumLagSeconds: maximum(row.maximumLagSeconds),
      }
    })
    .sort(
      (left, right) =>
        (latestMetricValue(right.allOperationsRate) ?? 0) - (latestMetricValue(left.allOperationsRate) ?? 0),
    )
}

export function latestMetricValue(points: MetricPoint[]) {
  return points.findLast(({ value }) => value !== null)?.value ?? null
}
