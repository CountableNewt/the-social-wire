import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const OPENAPI_PATH = join(import.meta.dir, "../openapi.yaml");
import { registeredGoRoutes } from "./helpers/goRoutes";

function extractOpenAPIPaths(yaml: string): string[] {
  const paths: string[] = [];
  for (const line of yaml.split("\n")) {
    const match = line.match(/^  (\/[^\s:]+):/);
    if (match) paths.push(match[1]!);
  }
  return paths;
}

describe("OpenAPI route drift", () => {
  it("keeps publisher content labels optional and aligned across reader and preview contracts", () => {
    const document = Bun.YAML.parse(readFileSync(OPENAPI_PATH, "utf8")) as any;
    const labels = document.components.schemas.ArticleContentLabels;
    expect(labels).toEqual(expect.objectContaining({ type: "array", maxItems: 2, uniqueItems: true }));
    expect(labels.items).toEqual({ type: "string", enum: ["ai-generated", "sexual"] });
    const schemas = [document.components.schemas.EntryListItem, document.components.schemas.EntryDetail,
      document.paths["/v1/publications/article-embed"].get.responses["200"].content["application/json"].schema];
    for (const schema of schemas) {
      expect(schema.properties.contentLabels).toEqual({ $ref: "#/components/schemas/ArticleContentLabels" });
      expect(schema.required).not.toContain("contentLabels");
    }
    const lexicon = JSON.parse(readFileSync(join(import.meta.dir, "../../lexicons/app/thesocialwire/publication/getArticleEmbed.json"), "utf8"));
    const output = lexicon.defs.main.output.schema;
    expect(output.required).not.toContain("contentLabels");
    expect(output.properties.contentLabels).toEqual(expect.objectContaining({ type: "array", maxLength: 2 }));
    expect(output.properties.contentLabels.items.knownValues).toEqual(labels.items.enum);
  });

  it("documents reconciled RSS source failures without treating enrollment as successful", () => {
    const document = Bun.YAML.parse(readFileSync(OPENAPI_PATH, "utf8")) as {
      paths: Record<string, { post: { responses: Record<string, { content?: Record<string, { schema: { $ref: string } }> }> } }>;
      components: { schemas: Record<string, { allOf: Array<{ properties?: Record<string, unknown> }> }> };
    };
    const enrollment = document.paths["/v1/appview/enroll"]!.post.responses;
    expect(enrollment["503"]?.content?.["application/json"]?.schema.$ref).toBe("#/components/schemas/AppViewEnrollmentError");
    const details = document.components.schemas.AppViewEnrollmentError!.allOf[1]!.properties!;
    expect(details.articlesUpdated).toEqual(expect.objectContaining({ type: "boolean", enum: [true] }));
    expect(details.sourceFailures).toEqual(expect.objectContaining({ type: "array" }));
    expect(details.unavailablePublicationIds).toEqual(expect.objectContaining({ type: "array", uniqueItems: true }));
  });

  it("keeps documented methods and paths aligned with Go registrations in both directions", () => {
    const document = Bun.YAML.parse(readFileSync(OPENAPI_PATH, "utf8")) as {
      paths: Record<string, Record<string, unknown>>;
    };
    const documented = Object.entries(document.paths).flatMap(([path, item]) =>
      Object.keys(item).filter(method => ["get", "post", "put", "patch", "delete"].includes(method))
        .map(method => `${method.toUpperCase()} ${path}`));
    // Canonical XRPC adapters inherit their REST compatibility contract via
    // the authoritative transport manifest rather than duplicating schemas.
    const manifest = JSON.parse(readFileSync(join(import.meta.dir, "../endpoint-manifest.json"), "utf8")) as {
      entries: { classification: string; xrpcNsid?: string }[];
    };
    for (const entry of manifest.entries) {
      if (entry.classification !== "xrpc-migration" || !entry.xrpcNsid) continue;
      const method = /\.(get|list|search)(?:[A-Z]|$)/.test(entry.xrpcNsid) ? "GET" : "POST";
      documented.push(`${method} /xrpc/${entry.xrpcNsid}`);
    }
    const registered = registeredGoRoutes();
    for (const route of documented) expect(registered.has(route), `unregistered contract: ${route}`).toBe(true);
    for (const route of registered) expect(documented, `undocumented Go route: ${route}`).toContain(route);
  });

  it("documents Semble ownership, pagination, and partial record-link contracts", () => {
    const document = Bun.YAML.parse(readFileSync(OPENAPI_PATH, "utf8")) as {
      paths: Record<string, Record<string, any>>;
      components: { schemas: Record<string, any> };
    };
    const schemas = document.components.schemas;

    expect(document.paths["/v1/semble/collections"].get.responses["403"]).toBeDefined();
    expect(document.paths["/v1/semble/collection"].get.responses["404"]).toBeDefined();
    for (const path of [
      "/v1/semble/collections",
      "/v1/semble/collection",
      "/v1/semble/connections",
    ]) {
      expect(document.paths[path].get.responses["502"], path).toBeDefined();
      expect(document.paths[path].get.parameters.some((item: any) => item.name === "cursor"), path)
        .toBe(true);
    }

    expect(schemas.SembleCollection.required).toEqual(["uri", "name", "cardCount"]);
    expect(schemas.SembleCollectionPage.required).toEqual([
      "collection",
      "items",
      "membershipComplete",
      "recordLinksComplete",
    ]);
    expect(schemas.SembleCollectionItem.required).toContain("membership");
    expect(schemas.SembleCollectionItem.required).toContain("unlinkAvailable");
    expect(schemas.SembleNote.required).toContain("uri");
    expect(schemas.SembleNote.properties.uri.type).toEqual(["string", "null"]);
    expect(schemas.SembleConnection.required).toContain("uri");
    expect(schemas.SembleConnection.properties.uri.type).toEqual(["string", "null"]);
    expect(schemas.SembleConnectionsPage.required).toContain("recordLinksComplete");
  });


});


describe("L@tr bookmark state transport compatibility", () => {
  it("documents and exercises the protected PATCH alias alongside canonical POST", () => {
    const yaml = readFileSync(OPENAPI_PATH, "utf8");
    const block = yaml.split("  /xrpc/link.latr.bookmarks.setState:\n")[1]?.split("\n  /xrpc/")[0] ?? "";
    expect(block).toContain("    post:");
    expect(block).toContain("    patch:");
    expect(block).toContain("operationId: setLatrBookmarkStatePatchCompatibility");
    expect(block.match(/responses:\n[\s\S]*?401/g)).toHaveLength(2);
    const routes = registeredGoRoutes();
    expect(routes.has("PATCH /xrpc/link.latr.bookmarks.setState")).toBe(true);
    expect(routes.has("POST /xrpc/link.latr.bookmarks.setState")).toBe(true);
    const bruno = readFileSync(join(import.meta.dir, "../../../services/appview/bruno/Latr/Set Bookmark State PATCH Compatibility.bru"), "utf8");
    expect(bruno).toContain("patch {");
    expect(bruno).toContain("X-Latr-Gateway-DPoP:");
  });
});

describe("Standard Reader list interoperability", () => {
  it("returns resolved creator identity independently of public list membership", () => {
    const spec = Bun.YAML.parse(readFileSync(OPENAPI_PATH, "utf8")) as any;
    const schema = spec.components.schemas.StandardReaderListsResponse;
    expect(schema.properties.creatorDid.type).toBe("string");
    expect(schema.required).not.toContain("creatorDid");
    expect(spec.paths["/v1/lists/search"].get.responses["200"].content["application/json"].schema.$ref)
      .toBe("#/components/schemas/StandardReaderListsResponse");
  });
  it("documents creator lookup, canonical resolution, feed scope, and opt-in bootstrap", () => {
    const spec = Bun.YAML.parse(readFileSync(OPENAPI_PATH, "utf8")) as any;
    expect(spec.paths["/v1/lists/search"].get.parameters[0].name).toBe("creator");
    expect(spec.paths["/v1/lists/resolve"].post.requestBody.content["application/json"].schema.required).toEqual(["input"]);
    expect(spec.paths["/v1/appview/feed"].get.parameters.find((p: any) => p.name === "kind").schema.enum).toContain("list");
    expect(spec.paths["/v1/appview/bootstrap-stream"].get.parameters.find((p: any) => p.name === "includeLists").schema.default).toBe(false);
    for (const service of ["appview"]) {
      for (const name of ["Get Lists", "Search Creator Lists", "Resolve List", "Refresh Lists"]) {
        expect(readFileSync(join(import.meta.dir, `../../../services/${service}/bruno/Lists/${name}.bru`), "utf8")).toContain("DPoP:");
      }
    }
  });
});
