import { expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const spec = Bun.YAML.parse(readFileSync(join(import.meta.dir, "../openapi.yaml"), "utf8")) as {
  paths: Record<string, Record<string, { security: unknown[]; responses: Record<string, unknown> }>>;
  components: { schemas: Record<string, { properties: Record<string, any>; required: string[] }> };
};
it("documents authenticated creator APIs with scoped nullable metrics and matching Bruno requests", () => {
  for (const [method, name, file] of [["get", "bluesky", "Get Bluesky Analytics"], ["get", "publications", "Get Publication Analytics"], ["get", "summary", "Get Creator Analytics"], ["post", "refresh", "Refresh Creator Analytics"], ["post", "events", "Record Creator Observation"]]) {
    const operation = spec.paths[`/v1/analytics/${name}`][method];
    expect(operation.security).toEqual([{ ATProtoOAuthDPoP: [] }]);
    expect(operation.responses["401"]).toBeDefined();
    for (const service of ["gateway", "appview"]) {
      const request = readFileSync(join(import.meta.dir, `../../../services/${service}/bruno/Analytics/${file}.bru`), "utf8");
      expect(request).toContain(`/v1/analytics/${name}`);
      expect(request).toContain("DPoP: {{dpopProof}}");
    }
  }
  const schemas = spec.components.schemas;
  expect(schemas.CreatorAnalyticsEvent.properties.event.enum).toEqual(["impression", "view", "outbound_click"]);
  expect(schemas.CreatorAnalyticsEvent.required).toEqual(["subjectUri", "event", "surface"]);
  expect(schemas.CreatorTracking.properties.ctr.type).toEqual(["number", "null"]);
  expect(schemas.CreatorEngagement.type).toEqual(["object", "null"]);
  expect(schemas.CreatorAnalyticsSummary.properties.coverage.properties.trackingScope.enum).toEqual(["social-wire"]);
  expect(schemas.CreatorAnalyticsSummary.properties.coverage.properties.platformImpressions.type).toBe("null");
});

it("defines Standard.site publication analytics with nullable counts and explicit coverage", () => {
  const operation = spec.paths["/v1/analytics/publications"].get;
  for (const status of ["200", "401", "403", "429", "502", "503"]) {
    expect(operation.responses[status]).toBeDefined();
  }
  const schemas = spec.components.schemas;
  expect(schemas.PublicationAnalyticsSummary.required).toEqual(["sampledAt", "publications", "inventoryComplete"]);
  expect(schemas.PublicationAnalyticsSummary.properties.publications.maxItems).toBe(20);
  expect(schemas.PublicationAnalyticsSummary.properties.inventoryComplete.type).toBe("boolean");
  const publication = schemas.PublicationAnalytics;
  expect(publication.required).toEqual(["uri", "name", "url", "documents", "subscribers", "recommendations", "recommendationCoverageComplete"]);
  expect(publication.properties.documents.type).toBe("integer");
  expect(publication.properties.subscribers.type).toEqual(["integer", "null"]);
  expect(publication.properties.recommendations.type).toEqual(["integer", "null"]);
  expect(publication.properties.recommendationCoverageComplete.type).toBe("boolean");
});

it("defines bounded Bluesky analysis with unknown metrics and explicit incomplete search coverage", () => {
  const operation = spec.paths["/v1/analytics/bluesky"].get;
  for (const status of ["200", "400", "401", "403", "429", "503"]) {
    expect(operation.responses[status]).toBeDefined();
  }
  const schemas = spec.components.schemas;
  const summary = schemas.BlueskyAnalyticsSummary;
  expect(summary.required).toEqual(["sampledAt", "days", "profile", "activity", "topPosts", "shares", "coverage"]);
  expect(summary.properties.days.enum).toEqual([7, 30, 90]);
  expect(summary.properties.profile.properties.followers.type).toEqual(["integer", "null"]);
  expect(summary.properties.activity.type).toEqual(["object", "null"]);
  expect(summary.properties.topPosts.maxItems).toBe(10);
  expect(summary.properties.topPosts.description).toContain("not interactions gained");
  expect(summary.properties.shares.maxItems).toBe(5);
  expect(summary.properties.coverage.properties.sharesComplete.const).toBe(false);
  expect(summary.properties.coverage.required).toContain("feedAvailable");
  expect(summary.properties.coverage.required).toContain("sharesAvailable");
  expect(summary.properties.coverage.properties.articlesSearched.maximum).toBe(5);
  expect(schemas.BlueskyAnalyticsPost.required).toContain("engagement");
});
