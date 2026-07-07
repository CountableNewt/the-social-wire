import { describe, expect, it } from "bun:test";
import { render, screen } from "@testing-library/react";

import {
  ReadLaterSidebarBadge,
  readLaterSavedCountLabel,
} from "@/components/AppSidebar/ReadLaterSidebarBadge";

describe("ReadLaterSidebarBadge", () => {
  it("formats singular and plural saved link labels", () => {
    expect(readLaterSavedCountLabel(1)).toBe("1 saved link");
    expect(readLaterSavedCountLabel(2)).toBe("2 saved links");
  });

  it("renders the active Read Later count", () => {
    render(<ReadLaterSidebarBadge count={3} />);

    expect(screen.getByLabelText("3 saved links").textContent).toBe("3");
  });

  it("hides zero counts", () => {
    const { container } = render(<ReadLaterSidebarBadge count={0} />);

    expect(container.childElementCount).toBe(0);
  });
});
