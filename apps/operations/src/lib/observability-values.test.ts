import { describe, expect, it } from "bun:test"
import { elapsedSeconds, overallSystemHealth, serviceHealthEvidence } from "@/lib/observability-values"
import { demoOverview } from "@/lib/demo-data"

describe("observability values", () => {
  it("derives health from every reporting service instead of a fixed label", () => {
    const evidence = serviceHealthEvidence(demoOverview.services, "freshness")

    expect(evidence.state).toBe("degraded")
    expect(evidence.healthy).toBe(3)
    expect(evidence.total).toBe(4)
    expect(overallSystemHealth(demoOverview)).toBe("unhealthy")
  })

  it("returns unknown when no service reports a dimension", () => {
    expect(serviceHealthEvidence([], "liveness").state).toBe("unknown")
  })

  it("withholds invalid or reversed timestamps", () => {
    expect(elapsedSeconds("2026-07-20T20:00:00.000Z", "2026-07-20T20:00:05.000Z")).toBe(5)
    expect(elapsedSeconds("invalid", "2026-07-20T20:00:05.000Z")).toBeNull()
    expect(elapsedSeconds("2026-07-20T20:00:05.000Z", "2026-07-20T20:00:00.000Z")).toBeNull()
  })
})
