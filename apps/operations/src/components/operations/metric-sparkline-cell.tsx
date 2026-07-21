import { Sparkline } from "@/components/operations/sparkline"
import { latestMetricValue, type MetricPoint } from "@/lib/collection-metrics"

export function MetricSparklineCell({
  points,
  label,
  format,
  tone = "primary",
}: {
  points: MetricPoint[]
  label: string
  format: (value: number) => string
  tone?: "primary" | "warning"
}) {
  const current = latestMetricValue(points)

  if (current === null) return <span className="text-muted-foreground">— No History</span>

  return (
    <div className="flex items-center gap-2 font-mono tabular-nums">
      <span>{format(current)}</span>
      <Sparkline points={points} label={`${label}, 15-minute trend`} format={format} tone={tone} />
    </div>
  )
}
