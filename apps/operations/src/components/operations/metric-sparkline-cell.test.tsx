import { afterEach, describe, expect, it } from "bun:test"
import { cleanup, render, screen } from "@testing-library/react"
import { MetricSparklineCell } from "@/components/operations/metric-sparkline-cell"

afterEach(cleanup)

describe("MetricSparklineCell", () => {
  it("does not present an older observation as the current bucket", () => {
    render(
      <MetricSparklineCell
        points={[
          { timestamp: 1, value: 12 },
          { timestamp: 2, value: null },
        ]}
        label="test rate"
        format={(value) => String(value)}
      />,
    )

    expect(screen.getByText("— Missing")).toBeTruthy()
    expect(screen.getByRole("img").getAttribute("aria-label")).toContain("zero baseline")
  })
})
