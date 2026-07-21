import { Activity, CheckCircle2, Clock3, Database, TriangleAlert } from "lucide-react"
import { elapsedSeconds, healthLabel, serviceHealthEvidence } from "@/lib/observability-values"
import type { Overview } from "@/lib/operations-types"

export function HealthStrip({ overview }: { overview: Overview }) {
  const liveness = serviceHealthEvidence(overview.services, "liveness")
  const readiness = serviceHealthEvidence(overview.services, "readiness")
  const freshness = serviceHealthEvidence(overview.services, "freshness")
  const completeness = serviceHealthEvidence(overview.services, "completeness")
  const commitAge = elapsedSeconds(overview.ingestion?.lastCommittedAt, overview.refreshedAt)
  const activeGaps = overview.gaps.filter((gap) => !["resolved", "ignored"].includes(gap.status)).length
  const ingestionFresh =
    overview.ingestion?.connectionState === "connected" &&
    commitAge !== null &&
    commitAge < 300 &&
    freshness.state === "healthy"
  const freshnessLabel =
    commitAge === null
      ? "Unknown"
      : overview.ingestion?.connectionState === "disconnected"
        ? "Disconnected"
        : freshness.state !== "healthy"
          ? healthLabel(freshness.state)
          : commitAge >= 300
            ? "Stale"
            : "Good"
  const projectionsComplete = completeness.state === "healthy" && activeGaps === 0
  const items = [
    {
      label: "Service Liveness",
      value: healthLabel(liveness.state),
      note: `${liveness.healthy} / ${liveness.total} instances report healthy`,
      icon: Activity,
      warning: liveness.state !== "healthy",
    },
    {
      label: "Traffic Readiness",
      value: readiness.state === "healthy" ? "Ready" : healthLabel(readiness.state),
      note: `${readiness.healthy} / ${readiness.total} instances report ready`,
      icon: CheckCircle2,
      warning: readiness.state !== "healthy",
    },
    {
      label: "Ingestion Freshness",
      value: freshnessLabel,
      note:
        commitAge === null
          ? "No valid committed-event timestamp reported"
          : `Last commit ${commitAge.toFixed(1)}s before refresh · service freshness ${freshness.state}`,
      icon: Clock3,
      warning: !ingestionFresh,
    },
    {
      label: "Projection Completeness",
      value: projectionsComplete ? "Complete" : completeness.state === "unknown" ? "Unknown" : "At Risk",
      note: `${activeGaps} active gaps · ${completeness.healthy} / ${completeness.total} instances complete`,
      icon: Database,
      warning: !projectionsComplete,
    },
  ]
  return (
    <section
      className="ops-panel grid divide-y sm:grid-cols-2 sm:divide-x sm:divide-y-0 xl:grid-cols-4"
      aria-label="System Health"
    >
      {items.map((item) => (
        <div key={item.label} className="relative min-w-0 p-3">
          <div className="flex items-center gap-2 text-[11px]">
            <item.icon className="size-3.5" />
            {item.label}
          </div>
          <p className={`mt-1 text-sm font-medium ${item.warning ? "ops-warning" : "ops-success"}`}>{item.value}</p>
          <p className="mt-1 text-[10px] text-muted-foreground">{item.note}</p>
          {item.warning ? <TriangleAlert className="absolute right-3 top-3 size-3.5 ops-warning" /> : null}
        </div>
      ))}
    </section>
  )
}
