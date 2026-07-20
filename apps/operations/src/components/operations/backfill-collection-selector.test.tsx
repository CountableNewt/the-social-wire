import { afterEach, describe, expect, it, mock } from "bun:test"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { BackfillCollectionSelector } from "@/components/operations/backfill-collection-selector"

afterEach(cleanup)

describe("BackfillCollectionSelector", () => {
  it("requires an explicit selection when the collection scope is unknown", () => {
    const onValueChange = mock(() => undefined)

    render(<BackfillCollectionSelector value={[]} onValueChange={onValueChange} />)

    expect(screen.getByRole("alert").textContent).toContain("Scope unknown")
    expect(screen.getAllByRole("checkbox").every((checkbox) => !(checkbox as HTMLInputElement).checked)).toBe(true)

    fireEvent.click(screen.getByRole("checkbox", { name: "site.standard.entry" }))

    expect(onValueChange).toHaveBeenCalledWith(["site.standard.entry"])
  })

  it("keeps an attributed collection selected", () => {
    render(<BackfillCollectionSelector value={["app.skyreader.feed.subscription"]} onValueChange={() => undefined} />)

    expect(screen.queryByRole("alert")).toBeNull()
    expect((screen.getByRole("checkbox", { name: "app.skyreader.feed.subscription" }) as HTMLInputElement).checked).toBe(true)
  })
})
