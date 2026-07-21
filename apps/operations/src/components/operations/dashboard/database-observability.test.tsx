import { afterEach, describe, expect, it } from "bun:test"
import { cleanup, render, screen } from "@testing-library/react"
import { DatabaseObservability } from "@/components/operations/dashboard/database-observability"
import { demoOverview } from "@/lib/demo-data"

afterEach(cleanup)

describe("DatabaseObservability", () => {
  it("summarizes database dependencies and correlated query spans", () => {
    render(<DatabaseObservability overview={demoOverview} />)

    expect(screen.getByText("Database Availability").nextElementSibling?.textContent).toBe("Ready")
    expect(screen.getByText("Database Request Volume").nextElementSibling?.textContent).toBe("4,826,341")
    expect(screen.getByText("Estimated Records", { selector: "p" }).nextElementSibling?.textContent).toBe("9,842,117")
    expect(screen.getByText("Connections").nextElementSibling?.textContent).toBe("8 / 15")
    expect(screen.getByText("Cache Hit Ratio").nextElementSibling?.textContent).toBe("99.7%")
  })

  it("withholds physically invalid ratios and counts", () => {
    render(
      <DatabaseObservability
        overview={{
          ...demoOverview,
          database: { ...demoOverview.database!, cacheHitRatio: 1.5, activeConnections: -1 },
        }}
      />,
    )

    expect(screen.getByText("Cache Hit Ratio").nextElementSibling?.textContent).toBe("—")
    expect(screen.getByText("Connections").nextElementSibling?.textContent).toBe("— / 15")
  })
})
