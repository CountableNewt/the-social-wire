import { afterEach, describe, expect, it } from "bun:test"
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { LiveStream } from "@/components/operations/dashboard/live-stream"
import { OperationsAuthProvider } from "@/lib/auth-context"
import { demoOverview } from "@/lib/demo-data"

afterEach(cleanup)
process.env.NEXT_PUBLIC_OPERATIONS_DEMO_MODE = "1"

describe("LiveStream", () => {
  function renderStream(data = demoOverview) {
    const queryClient = new QueryClient()
    return render(
      <QueryClientProvider client={queryClient}>
        <OperationsAuthProvider>
          <LiveStream data={data} environment="development" />
        </OperationsAuthProvider>
      </QueryClientProvider>,
    )
  }

  it("shows only values present in stream telemetry or derived from its timestamps", () => {
    renderStream()

    expect(screen.getByText("jetstream")).toBeTruthy()
    expect(screen.getByText("2,100,333 μs")).toBeTruthy()
    expect(screen.queryByText("dev-js-03")).toBeNull()
    expect(screen.queryByText("410 ms / 1.82 s")).toBeNull()
  })

  it("reflects disconnected state instead of a fixed connected badge", () => {
    renderStream({ ...demoOverview, ingestion: { ...demoOverview.ingestion!, connectionState: "disconnected" } })

    expect(screen.getByText("● disconnected")).toBeTruthy()
  })

  it("shows both Jetstream endpoints and the active failover role", () => {
    renderStream()

    expect(screen.getByText("Jetstream 1")).toBeTruthy()
    expect(screen.getByText("Jetstream 2")).toBeTruthy()
    expect(screen.getByText("active")).toBeTruthy()
    expect(screen.getByText("standby")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Reconnect Jetstream" })).toBeTruthy()
  })

  it("does not require an operator reason to reconnect a disconnected Jetstream", () => {
    renderStream({ ...demoOverview, ingestion: { ...demoOverview.ingestion!, connectionState: "disconnected" } })

    fireEvent.click(screen.getByRole("button", { name: "Reconnect Jetstream" }))
    const dialog = screen.getByRole("dialog", { name: "Reconnect Jetstream?" })
    expect(within(dialog).queryByLabelText("Operator Audit Note")).toBeNull()
    expect(within(dialog).getByRole("button", { name: "Reconnect Jetstream" }).hasAttribute("disabled")).toBe(false)
  })

  it("shows reconnect progress instead of offering a duplicate command", () => {
    renderStream({
      ...demoOverview,
      commands: [
        {
          id: "command-1",
          action: "reconnect_jetstream",
          status: "running",
          requestedByDid: "did:plc:operator",
          auditNote: "Reconnect stalled ingestion",
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        },
      ],
    })

    expect(screen.getByText("Reconnect running")).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Reconnect Jetstream" })).toBeNull()
  })
})
