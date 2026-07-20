import { describe, expect, it } from "bun:test"
import { render, screen } from "@testing-library/react"
import { DatabaseObservability } from "@/components/operations/database-observability"
import { demoOverview } from "@/lib/demo-data"

describe("DatabaseObservability", () => {
  it("summarizes database dependencies and correlated query spans", () => {
    render(<DatabaseObservability overview={demoOverview} />)

    expect(screen.getByText("Database Availability").nextElementSibling?.textContent).toBe("Ready")
    expect(screen.getByText("Database Request Volume").nextElementSibling?.textContent).toBe("4,826,341")
    expect(screen.getByText("Estimated Records", { selector: "p" }).nextElementSibling?.textContent).toBe("9,842,117")
    expect(screen.getByText("Connections").nextElementSibling?.textContent).toBe("8 / 15")
    expect(screen.getByText("Cache Hit Ratio").nextElementSibling?.textContent).toBe("99.7%")
  })
})
