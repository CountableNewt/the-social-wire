import { describe, expect, it } from "bun:test"
import { collectionMetricRows, latestMetricValue } from "@/lib/collection-metrics"
import type { MetricRollup } from "@/lib/operations-types"

function rollup(overrides: Partial<MetricRollup> = {}): MetricRollup {
  return {
    bucketStart: "2026-07-20T20:00:00.000Z",
    metricName: "socialwire.ingestion.events_total",
    dimensions: { collection: "site.standard.document", operation: "create", ingestion_mode: "live" },
    sampleCount: 120,
    valueSum: 120,
    valueMin: 1,
    valueMax: 1,
    ...overrides,
  }
}

describe("collectionMetricRows", () => {
  it("derives per-second trends from retained one-minute counter buckets", () => {
    const rows = collectionMetricRows([
      rollup(),
      rollup({ bucketStart: "2026-07-20T20:01:00.000Z", sampleCount: 240, valueSum: 240 }),
    ])

    expect(rows).toHaveLength(1)
    expect(rows[0]?.createRate.map(({ value }) => value)).toEqual([2, 4])
    expect(rows[0]?.allOperationsRate.map(({ value }) => value)).toEqual([2, 4])
  })

  it("uses explicit zeroes for missing counter buckets instead of carrying stale failures forward", () => {
    const rows = collectionMetricRows([
      rollup(),
      rollup({
        metricName: "socialwire.ingestion.results_total",
        dimensions: {
          collection: "site.standard.document",
          operation: "create",
          result: "error",
          ingestion_mode: "live",
        },
        sampleCount: 6,
        valueSum: 6,
      }),
      rollup({ bucketStart: "2026-07-20T20:01:00.000Z", sampleCount: 240, valueSum: 240 }),
    ])

    expect(rows[0]?.failedRate.map(({ value }) => value)).toEqual([0.1, 0])
    expect(latestMetricValue(rows[0]!.failedRate)).toBe(0)
  })

  it("excludes reconciliation metrics from the live ingestion trend", () => {
    const rows = collectionMetricRows([
      rollup({ dimensions: { collection: "site.standard.document", operation: "create", ingestion_mode: "backfill" } }),
    ])

    expect(rows).toEqual([])
  })
})
