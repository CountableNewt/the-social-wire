import { expect, it } from "bun:test";
import { financeSidebarInterests, sportsSidebarInterests } from "@/hooks/useTopicSidebarInterests";
import type { FinanceFeedDefinition, FinanceSelection } from "@/lib/financeFeedClient";
import type { SportsEntity, SportsFeedDefinition, SportsSelection } from "@/lib/sportsFeedClient";

it("includes followed Finance securities and sectors, excluding unknown and mixed feeds", () => {
  const definitions: FinanceFeedDefinition[] = [
    { id: "apple", title: "$AAPL · Apple · NASDAQ", kind: "instrument", instrumentIDs: ["apple"], sectorIDs: [], description: "" },
    { id: "tech", title: "Technology", kind: "industry", instrumentIDs: [], sectorIDs: ["tech"], description: "" },
    { id: "mixed", title: "Mixed", kind: "industry", instrumentIDs: [], sectorIDs: ["tech", "energy"], description: "" },
    { id: "other", title: "Other", kind: "instrument", instrumentIDs: ["other"], sectorIDs: [], description: "" },
  ];
  const selections: FinanceSelection[] = [
    { kind: "instrument", reference: "apple", createdAt: "now", updatedAt: "now" },
    { kind: "sector", reference: "tech", createdAt: "now", updatedAt: "now" },
    { kind: "instrument", reference: "unknown", createdAt: "now", updatedAt: "now" },
  ];
  expect(financeSidebarInterests(definitions, selections)).toEqual([
    { id: "apple", title: "$AAPL · Apple" }, { id: "tech", title: "Technology" },
  ]);
  expect(financeSidebarInterests(definitions, [])).toEqual([]);
});

it("includes all followed Sports kinds while excluding muted, inactive, unresolved interests", () => {
  const entities: SportsEntity[] = ["sport", "competition", "team", "athlete", "driver", "classification"].map(kind => ({ id: kind, name: kind, kind, active: true, competitionIDs: [], aliases: [] }));
  entities.push({ id: "inactive", name: "Inactive", kind: "team", active: false, competitionIDs: [], aliases: [] });
  const definitions: SportsFeedDefinition[] = entities.map(entity => ({ id: `entity:${entity.id}`, title: entity.name, kind: entity.kind, entityIDs: [entity.id], description: "" }));
  const selections: SportsSelection[] = [...entities.map(entity => ({ reference: entity.id, action: "follow" as const, createdAt: "now", updatedAt: "now" })), { reference: "competition", action: "mute", createdAt: "now", updatedAt: "now" }, { reference: "missing", action: "follow", createdAt: "now", updatedAt: "now" }];
  expect(sportsSidebarInterests(entities, definitions, selections, "en-US").map(row => row.id)).toEqual(["entity:athlete", "entity:classification", "entity:driver", "entity:sport", "entity:team"]);
  expect(sportsSidebarInterests(entities, [], selections, "en-US")).toEqual([]);
});
