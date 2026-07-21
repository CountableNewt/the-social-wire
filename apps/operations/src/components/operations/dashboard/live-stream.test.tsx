import { afterEach, describe, expect, it } from "bun:test"
import { cleanup, render, screen } from "@testing-library/react"
import { LiveStream } from "@/components/operations/dashboard/live-stream"
import { demoOverview } from "@/lib/demo-data"

afterEach(cleanup)

describe("LiveStream", () => {
  it("shows only values present in stream telemetry or derived from its timestamps", () => {
    render(<LiveStream data={demoOverview} />)

    expect(screen.getByText("jetstream")).toBeTruthy()
    expect(screen.getByText("2,100,333 μs")).toBeTruthy()
    expect(screen.queryByText("dev-js-03")).toBeNull()
    expect(screen.queryByText("410 ms / 1.82 s")).toBeNull()
  })

  it("reflects disconnected state instead of a fixed connected badge", () => {
    render(
      <LiveStream
        data={{ ...demoOverview, ingestion: { ...demoOverview.ingestion!, connectionState: "disconnected" } }}
      />,
    )

    expect(screen.getByText("● disconnected")).toBeTruthy()
  })
})
