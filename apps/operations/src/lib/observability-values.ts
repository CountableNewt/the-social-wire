import type { Health, Overview, ServiceState } from "@/lib/operations-types"

export type HealthDimension = "liveness" | "readiness" | "freshness" | "completeness"

export type HealthEvidence = {
  state: Health
  healthy: number
  total: number
}

export function boundedNonNegativeNumber(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : null
}

export function boundedNonNegativeInteger(value: unknown): number | null {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0 ? value : null
}

export function elapsedSeconds(start: string | undefined, end: string): number | null {
  if (!start) return null
  const startMs = new Date(start).getTime()
  const endMs = new Date(end).getTime()
  if (!Number.isFinite(startMs) || !Number.isFinite(endMs) || endMs < startMs) return null
  return (endMs - startMs) / 1_000
}

export function serviceHealthEvidence(services: ServiceState[], dimension: HealthDimension): HealthEvidence {
  const states = services.map((service) => service[dimension])
  const healthy = states.filter((state) => state === "healthy").length
  const state: Health =
    states.length === 0 || states.some((value) => value === "unknown")
      ? "unknown"
      : states.some((value) => value === "unhealthy")
        ? "unhealthy"
        : states.some((value) => value === "degraded")
          ? "degraded"
          : "healthy"
  return { state, healthy, total: states.length }
}

export function healthLabel(state: Health) {
  if (state === "healthy") return "Healthy"
  if (state === "degraded") return "Degraded"
  if (state === "unhealthy") return "Unhealthy"
  return "Unknown"
}

export function overallSystemHealth(overview: Overview): Health {
  const dimensions: HealthDimension[] = ["liveness", "readiness", "freshness", "completeness"]
  const states = dimensions.map((dimension) => serviceHealthEvidence(overview.services, dimension).state)
  if (
    overview.ingestion?.connectionState === "disconnected" ||
    overview.alerts.some((alert) => alert.status === "open" && alert.severity === "critical") ||
    states.includes("unhealthy")
  )
    return "unhealthy"
  if (
    overview.ingestion?.connectionState === "reconnecting" ||
    overview.gaps.some((gap) => !["resolved", "ignored"].includes(gap.status)) ||
    overview.alerts.some((alert) => alert.status !== "resolved") ||
    states.includes("degraded")
  )
    return "degraded"
  if (!overview.ingestion || overview.ingestion.connectionState === "unknown" || states.includes("unknown")) return "unknown"
  return "healthy"
}
