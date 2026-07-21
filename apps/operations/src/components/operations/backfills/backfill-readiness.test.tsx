import { afterEach, describe, expect, it } from "bun:test"
import { cleanup, render, screen } from "@testing-library/react"
import { BackfillReadiness } from "@/components/operations/backfills/backfill-readiness"

afterEach(cleanup)

describe("BackfillReadiness", () => {
  it("identifies the unmet requirements", () => {
    render(
      <BackfillReadiness
        input={{
          collectionScopeSelected: true,
          dryRunComplete: true,
          reviewed: false,
          environment: "production",
          environmentConfirmation: "production",
          pending: false,
        }}
      />,
    )

    expect(screen.getByText("2 of 4 Complete")).toBeTruthy()
    expect(screen.getByText("Production confirmation exactly matches PRODUCTION").parentElement?.className).toContain(
      "text-muted-foreground",
    )
  })

  it("reports when all requirements are met", () => {
    render(
      <BackfillReadiness
        input={{
          collectionScopeSelected: true,
          dryRunComplete: true,
          reviewed: true,
          environment: "development",
          environmentConfirmation: "",
          pending: false,
        }}
      />,
    )

    expect(screen.getByText("All Requirements Met")).toBeTruthy()
  })
})
