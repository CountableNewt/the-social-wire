import { describe, expect, it, mock } from "bun:test";
import { render, screen } from "@testing-library/react";

import type { DiscoveredPublication } from "@/lib/atprotoClient";

mock.module("web-haptics/react", () => ({
  useWebHaptics: () => ({
    isSupported: false,
    trigger: () => undefined,
  }),
}));

function publication(index: number): DiscoveredPublication {
  return {
    publicationId: `pub-${index}`,
    authorDid: `did:plc:author${index}`,
    authorHandle: `author${index}.example`,
    title: `Publication ${index}`,
    discoveredAt: "2026-01-01T00:00:00.000Z",
  };
}

describe("PublicationMenuSubEntries", () => {
  it("virtualizes large publication lists", async () => {
    const { PublicationMenuSubEntries } = await import(
      "@/components/AppSidebar/PublicationMenuSubEntries"
    );

    render(
      <ul>
        <PublicationMenuSubEntries
          publications={Array.from({ length: 120 }, (_, index) =>
            publication(index)
          )}
          publicationUnreadCounts={new Map()}
          selectedPubId={null}
          onSelectPub={() => undefined}
          folders={[]}
          prefsMap={new Map()}
          sidebarTab="subscribed"
        />
      </ul>
    );

    expect(screen.getByTestId("virtualized-publication-list")).toBeDefined();
    expect(screen.queryAllByRole("button").length).toBeLessThan(120);
  });
});
