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
  if (
    !Number.isFinite(rollup.valueSum) ||
    rollup.valueSum < 0 ||
    !Number.isSafeInteger(rollup.sampleCount) ||
    rollup.sampleCount < 0 ||
    (rollup.valueMax !== undefined && (!Number.isFinite(rollup.valueMax) || rollup.valueMax < 0))
  )
    return
  const current = series.get(timestamp) ?? { sum: 0, count: 0, maximum: null }
  current.sum += rollup.valueSum
  current.count += rollup.sampleCount
  if (rollup.valueMax !== undefined) current.maximum = Math.max(current.maximum ?? -Infinity, rollup.valueMax)
  series.set(timestamp, current)
}

function observedTimestamps(row: MutableCollectionMetricRow) {
  return new Set(
    Object.entries(row)
      .filter((entry): entry is [string, Series] => entry[1] instanceof Map)
      .flatMap(([, series]) => [...series.keys()]),
  )
}

function counterRate(series: Series, timestamps: number[], observed: Set<number>) {
  return timestamps.map((timestamp) => ({
    timestamp,
    value: observed.has(timestamp) ? (series.get(timestamp)?.sum ?? 0) / 60 : null,
  }))
}

function average(series: Series, timestamps: number[], multiplier = 1) {
  return timestamps.map((timestamp) => {
    const aggregate = series.get(timestamp)
    return {
      timestamp,
      value: aggregate && aggregate.count > 0 ? (aggregate.sum / aggregate.count) * multiplier : null,
    }
  })
}

function maximum(series: Series, timestamps: number[], multiplier = 1) {
  return timestamps.map((timestamp) => {
    const value = series.get(timestamp)?.maximum
    return { timestamp, value: value === null || value === undefined ? null : value * multiplier }
  })
}

function metricTimestamps(rows: Map<string, MutableCollectionMetricRow>, refreshedAt?: string) {
  const refreshedAtMs = refreshedAt ? new Date(refreshedAt).getTime() : Number.NaN
  if (Number.isFinite(refreshedAtMs)) {
    const finalClosedBucket = Math.floor(refreshedAtMs / 60_000) * 60_000 - 60_000
    return Array.from({ length: 15 }, (_, index) => finalClosedBucket - (14 - index) * 60_000)
  }

  return [...new Set([...rows.values()].flatMap((row) => [...observedTimestamps(row)]))].sort(
    (left, right) => left - right,
  )
}

export function collectionMetricRows(rollups: MetricRollup[], refreshedAt?: string): CollectionMetricRow[] {
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

  const timestamps = metricTimestamps(rows, refreshedAt)

  return [...rows.values()]
    .map((row) => {
      const observed = observedTimestamps(row)
      return {
        collection: row.collection,
        createRate: counterRate(row.createRate, timestamps, observed),
        updateRate: counterRate(row.updateRate, timestamps, observed),
        deleteRate: counterRate(row.deleteRate, timestamps, observed),
        allOperationsRate: counterRate(row.allOperationsRate, timestamps, observed),
        acceptedRate: counterRate(row.acceptedRate, timestamps, observed),
        failedRate: counterRate(row.failedRate, timestamps, observed),
        averageCommitMilliseconds: average(row.averageCommitMilliseconds, timestamps, 1_000),
        maximumCommitMilliseconds: maximum(row.maximumCommitMilliseconds, timestamps, 1_000),
        averageLagSeconds: average(row.averageLagSeconds, timestamps),
        maximumLagSeconds: maximum(row.maximumLagSeconds, timestamps),
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

export function currentMetricValue(points: MetricPoint[]) {
  return points.at(-1)?.value ?? null
}
