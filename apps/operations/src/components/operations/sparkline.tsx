import type { MetricPoint } from "@/lib/collection-metrics"
import { sparklinePaths } from "@/lib/sparkline-path"

export function Sparkline({
  points,
  label,
  tone = "primary",
}: {
  points: MetricPoint[]
  label: string
  tone?: "primary" | "warning"
}) {
  const paths = sparklinePaths(points)
  const singlePoint = points.filter(({ value }) => value !== null).length === 1

  return (
    <svg className="h-5 w-20" viewBox="0 0 80 20" role="img" aria-label={label}>
      <title>{label}</title>
      {paths.map((path) => (
        <path
          key={path}
          d={path}
          fill="none"
          stroke={tone === "warning" ? "var(--warning)" : "var(--primary)"}
          strokeWidth="1.6"
          vectorEffect="non-scaling-stroke"
        />
      ))}
      {singlePoint ? (
        <circle cx="40" cy="10" r="2" fill={tone === "warning" ? "var(--warning)" : "var(--primary)"} />
      ) : null}
    </svg>
  )
}
