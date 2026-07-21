import { afterEach, describe, expect, it } from "bun:test"
import { cleanup, render, screen } from "@testing-library/react"
import { HealthStrip } from "@/components/operations/dashboard/health-strip"
import { demoOverview } from "@/lib/demo-data"

afterEach(cleanup)

describe("HealthStrip", () => {
  it("uses descriptive health dimension titles", () => {
    render(<HealthStrip overview={demoOverview} />)

    expect(screen.getByText("Service Liveness")).toBeTruthy()
    expect(screen.getByText("Traffic Readiness")).toBeTruthy()
    expect(screen.getByText("Ingestion Freshness")).toBeTruthy()
    expect(screen.getByText("Projection Completeness")).toBeTruthy()
  })

  it("renders service health and gap counts from reported evidence", () => {
    render(<HealthStrip overview={demoOverview} />)

    expect(screen.getByText("4 / 4 instances report healthy")).toBeTruthy()
    expect(screen.getByText(/3 active gaps · 3 \/ 4 instances complete/)).toBeTruthy()
    expect(screen.getAllByText("Degraded").length).toBeGreaterThan(0)
  })

  it("does not claim fresh ingestion when the committed timestamp is missing", () => {
    render(<HealthStrip overview={{ ...demoOverview, ingestion: { ...demoOverview.ingestion!, lastCommittedAt: undefined } }} />)

    expect(screen.getByText("No valid committed-event timestamp reported")).toBeTruthy()
    expect(screen.getByText("Unknown")).toBeTruthy()
  })
})
