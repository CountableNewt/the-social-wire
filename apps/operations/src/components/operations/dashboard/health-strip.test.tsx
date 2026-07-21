import { describe, expect, it } from "bun:test"
import { render, screen } from "@testing-library/react"
import { HealthStrip } from "@/components/operations/dashboard/health-strip"
import { demoOverview } from "@/lib/demo-data"

describe("HealthStrip", () => {
  it("uses descriptive health dimension titles", () => {
    render(<HealthStrip overview={demoOverview} />)

    expect(screen.getByText("Service Liveness")).toBeTruthy()
    expect(screen.getByText("Traffic Readiness")).toBeTruthy()
    expect(screen.getByText("Ingestion Freshness")).toBeTruthy()
    expect(screen.getByText("Projection Completeness")).toBeTruthy()
  })
})
