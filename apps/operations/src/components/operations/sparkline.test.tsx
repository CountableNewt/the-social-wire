import { afterEach, describe, expect, it } from "bun:test"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { Sparkline } from "@/components/operations/sparkline"

afterEach(cleanup)

const points = [
  { timestamp: Date.parse("2026-07-20T20:00:00.000Z"), value: 2 },
  { timestamp: Date.parse("2026-07-20T20:01:00.000Z"), value: 7 },
]

describe("Sparkline", () => {
  it("shows the nearest real metric point on hover", () => {
    render(<Sparkline points={points} label="Create rate trend" format={(value) => `${value} eps`} />)
    const chart = screen.getByRole("img", { name: "Create rate trend" })
    chart.getBoundingClientRect = () => ({
      bottom: 20,
      height: 20,
      left: 0,
      right: 80,
      top: 0,
      width: 80,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    })

    fireEvent.mouseEnter(chart, { clientX: 2 })

    const tooltip = screen.getByRole("tooltip")
    expect(tooltip.textContent).toContain("2 eps")
    expect(tooltip.querySelector("time")?.getAttribute("datetime")).toBe("2026-07-20T20:00:00.000Z")
  })

  it("supports keyboard traversal across real metric points", () => {
    render(<Sparkline points={points} label="Create rate trend" format={(value) => `${value} eps`} />)
    const chart = screen.getByRole("img", { name: "Create rate trend" })

    fireEvent.focus(chart)
    expect(screen.getByRole("tooltip").textContent).toContain("7 eps")

    fireEvent.keyDown(chart, { key: "ArrowLeft" })
    expect(screen.getByRole("tooltip").textContent).toContain("2 eps")
  })
})
