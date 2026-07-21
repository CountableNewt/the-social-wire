import type { MetricPoint } from "@/lib/collection-metrics"

export type SparklinePoint = {
  timestamp: number
  value: number
  x: number
  y: number
}

export function sparklineGeometry(points: MetricPoint[], width = 80, height = 20) {
  const boundedPoints = points.map((point) => ({
    timestamp: point.timestamp,
    value:
      Number.isFinite(point.timestamp) && point.value !== null && Number.isFinite(point.value) && point.value >= 0
        ? point.value
        : null,
  }))
  const visible = boundedPoints.filter((point): point is { timestamp: number; value: number } => point.value !== null)
  if (visible.length === 0) return { paths: [], points: [] }

  const minimumTimestamp = Math.min(...visible.map(({ timestamp }) => timestamp))
  const maximumTimestamp = Math.max(...visible.map(({ timestamp }) => timestamp))
  const minimumValue = 0
  const maximumValue = Math.max(...visible.map(({ value }) => value))
  const horizontalRange = maximumTimestamp - minimumTimestamp
  const verticalRange = maximumValue - minimumValue
  const horizontalPadding = 1
  const verticalPadding = 2
  const paths: string[] = []
  const positionedPoints: SparklinePoint[] = []
  let current = ""

  for (const point of boundedPoints) {
    if (point.value === null) {
      if (current) paths.push(current)
      current = ""
      continue
    }

    const x =
      horizontalRange === 0
        ? width / 2
        : horizontalPadding + ((point.timestamp - minimumTimestamp) / horizontalRange) * (width - horizontalPadding * 2)
    const y =
      verticalRange === 0
        ? height - verticalPadding
        : height - verticalPadding - ((point.value - minimumValue) / verticalRange) * (height - verticalPadding * 2)
    positionedPoints.push({ timestamp: point.timestamp, value: point.value, x, y })
    current += `${current ? " L" : "M"}${x.toFixed(2)} ${y.toFixed(2)}`
  }

  if (current) paths.push(current)
  return { paths, points: positionedPoints }
}

export function sparklinePaths(points: MetricPoint[], width = 80, height = 20) {
  return sparklineGeometry(points, width, height).paths
}
