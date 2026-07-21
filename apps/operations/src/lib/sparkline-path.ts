import type { MetricPoint } from "@/lib/collection-metrics"

export function sparklinePaths(points: MetricPoint[], width = 80, height = 20) {
  const visible = points.filter((point): point is { timestamp: number; value: number } => point.value !== null)
  if (visible.length === 0) return []

  const minimumTimestamp = Math.min(...visible.map(({ timestamp }) => timestamp))
  const maximumTimestamp = Math.max(...visible.map(({ timestamp }) => timestamp))
  const minimumValue = Math.min(...visible.map(({ value }) => value))
  const maximumValue = Math.max(...visible.map(({ value }) => value))
  const horizontalRange = maximumTimestamp - minimumTimestamp
  const verticalRange = maximumValue - minimumValue
  const horizontalPadding = 1
  const verticalPadding = 2
  const paths: string[] = []
  let current = ""

  for (const point of points) {
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
        ? height / 2
        : height - verticalPadding - ((point.value - minimumValue) / verticalRange) * (height - verticalPadding * 2)
    current += `${current ? " L" : "M"}${x.toFixed(2)} ${y.toFixed(2)}`
  }

  if (current) paths.push(current)
  return paths
}
