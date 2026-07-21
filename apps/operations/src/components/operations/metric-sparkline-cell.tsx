import { Sparkline } from "@/components/operations/sparkline"
import { currentMetricValue, latestMetricValue, type MetricPoint } from "@/lib/collection-metrics"

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
  const current = currentMetricValue(points)
  const hasHistory = latestMetricValue(points) !== null

  if (!hasHistory) return <span className="text-muted-foreground">— No History</span>

  return (
    <div className="flex items-center gap-2 font-mono tabular-nums">
      <span className={current === null ? "text-muted-foreground" : undefined}>
        {current === null ? "— Missing" : format(current)}
      </span>
      <Sparkline points={points} label={`${label}, observed 15-minute trend with zero baseline`} format={format} tone={tone} />
    </div>
  )
}
