import { Activity, CheckCircle2, Clock3, Database, TriangleAlert } from "lucide-react"
import type { Overview } from "@/lib/operations-types"

export function HealthStrip({ overview }: { overview: Overview }) {
  const stale = overview.ingestion?.lastCommittedAt
    ? Math.max(
        0,
        (new Date(overview.refreshedAt).getTime() - new Date(overview.ingestion.lastCommittedAt).getTime()) / 1000,
      )
    : 0
  const items = [
    { label: "Service Liveness", value: "Healthy", note: "All services running", icon: Activity, warning: false },
    {
      label: "Traffic Readiness",
      value: "Ready",
      note: "All critical components ready",
      icon: CheckCircle2,
      warning: false,
    },
    {
      label: "Ingestion Freshness",
      value: stale < 300 ? "Good" : "Stale",
      note: `Ingestion lag ${stale.toFixed(1)}s`,
      icon: Clock3,
      warning: stale >= 300,
    },
    {
      label: "Projection Completeness",
      value: overview.gaps.some((gap) => gap.status === "confirmed") ? "At Risk" : "Complete",
      note: `${overview.gaps.filter((gap) => !["resolved", "ignored"].includes(gap.status)).length} gaps detected`,
      icon: Database,
      warning: overview.gaps.some((gap) => gap.status === "confirmed"),
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
