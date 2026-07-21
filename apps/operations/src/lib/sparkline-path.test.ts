import { describe, expect, it } from "bun:test"
import { sparklinePaths } from "@/lib/sparkline-path"

describe("sparklinePaths", () => {
  it("maps timestamped values to distinct chart geometry", () => {
    const rising = sparklinePaths([
      { timestamp: 0, value: 1 },
      { timestamp: 10, value: 2 },
      { timestamp: 20, value: 4 },
    ])
    const falling = sparklinePaths([
      { timestamp: 0, value: 4 },
      { timestamp: 10, value: 2 },
      { timestamp: 20, value: 1 },
    ])

    expect(rising).not.toEqual(falling)
    expect(rising[0]).toStartWith("M1.00 18.00")
    expect(falling[0]).toStartWith("M1.00 2.00")
  })

  it("breaks the path when history contains a missing bucket", () => {
    expect(
      sparklinePaths([
        { timestamp: 0, value: 1 },
        { timestamp: 10, value: null },
        { timestamp: 20, value: 2 },
      ]),
    ).toHaveLength(2)
  })
})
