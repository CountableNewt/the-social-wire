import { describe, expect, it } from "bun:test";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

const OPENAPI_PATH = join(import.meta.dir, "../openapi.yaml");
const GATEWAY_SOURCES = join(
  import.meta.dir,
  "../../../services/gateway/Sources/Gateway/Routes"
);
const OPERATIONS_PROXY_ROUTES = join(GATEWAY_SOURCES, "OperationsProxyRoutes.swift");

function collectRouterFiles(dir: string): string[] {
  const entries = readdirSync(dir, { withFileTypes: true });
  const files: string[] = [];
  for (const entry of entries) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...collectRouterFiles(full));
    } else if (entry.name.endsWith(".swift")) {
      files.push(full);
    }
  }
  return files;
}

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

  it("documents paths registered in gateway and appview router sources", () => {
    const yaml = readFileSync(OPENAPI_PATH, "utf8");
    const routerSources = [
      ...collectRouterFiles(join(import.meta.dir, "../../../services/gateway/Sources")),
      ...collectRouterFiles(join(import.meta.dir, "../../../services/appview/Sources")),
    ]
      .map((file) => readFileSync(file, "utf8"))
      .join("\n");
    const paths = extractOpenAPIPaths(yaml);

    const escape = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const registered = (path: string) => {
      // Hummingbird routes spell parameters as :name, and loops interpolate the final
      // path or NSID segment from a literal name list.
      const route = path.replace(/\{([A-Za-z][A-Za-z0-9_]*)\}/g, ":$1");
      if (new RegExp(`"${escape(route)}"`).test(routerSources)) return true;
      const split = Math.max(route.lastIndexOf("/"), route.startsWith("/xrpc/") ? route.lastIndexOf(".") : -1);
      const [prefix, segment] = [route.slice(0, split + 1), route.slice(split + 1)];
      return routerSources.includes(`"${prefix}\\(`) && routerSources.includes(`"${segment}"`);
    };
    for (const path of paths) expect(registered(path), path).toBe(true);
  });

  it("documents every directly registered literal /v1 path", () => {
    const documented = new Set(extractOpenAPIPaths(readFileSync(OPENAPI_PATH, "utf8")));
    const routeFiles = [
      ...collectRouterFiles(join(import.meta.dir, "../../../services/gateway/Sources")),
      ...collectRouterFiles(join(import.meta.dir, "../../../services/appview/Sources")),
    ];
    const directlyRegistered = routeFiles.flatMap((file) => {
      const source = readFileSync(file, "utf8");
      return [...source.matchAll(/(?:Handle(?:Func)?\("(?:GET|POST|PUT|DELETE|PATCH) |register\(mux, "(?:GET|POST|PUT|DELETE|PATCH)", "|\.(?:get|post|put|delete|patch)\(")([^"\n]+)"/g)]
        .map((match) => match[1]!)
        .filter((path) => path.startsWith("/v1/") && !path.endsWith("/") && !path.includes("\\("))
        .map((path) => path.replace(/:([A-Za-z][A-Za-z0-9_]*)/g, "{$1}"));
    });

    expect(directlyRegistered.length).toBeGreaterThan(0);
    for (const path of directlyRegistered) {
      expect(documented.has(path), path).toBe(true);
    }
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

  it("defines machine-readable Operations request, response, evidence, and concurrency contracts", () => {
    const yaml = readFileSync(OPENAPI_PATH, "utf8");
    const document = Bun.YAML.parse(yaml) as {
      paths: Record<string, Record<string, any>>;
      components: {
        schemas: Record<string, any>;
        parameters: Record<string, any>;
      };
    };

    const operationsPaths = Object.entries(document.paths).filter(([path]) =>
      path.startsWith("/v1/operations/")
    );
    expect(operationsPaths.length).toBeGreaterThan(10);

    for (const [path, pathItem] of operationsPaths) {
      for (const [method, operation] of Object.entries(pathItem)) {
        if (!['get', 'post', 'patch'].includes(method)) continue;
        expect(operation.operationId, `${method.toUpperCase()} ${path}`).toBeString();
        expect(operation.responses, `${method.toUpperCase()} ${path}`).toBeDefined();

        const success = operation.responses['200'] ?? operation.responses['201'] ?? operation.responses['202'];
        expect(success, `${method.toUpperCase()} ${path}`).toBeDefined();
        const content = success.content;
        if (path === "/v1/operations/events/stream") {
          expect(content?.['text/event-stream']?.schema?.type).toBe("string");
        } else {
          expect(content?.['application/json']?.schema, `${method.toUpperCase()} ${path}`).toBeDefined();
        }
      }
    }

    const schemas = document.components.schemas;
    expect(schemas.OperationsEvidenceMetadata.required).toEqual([
      "source",
      "accuracy",
      "generatedAt",
      "ageSeconds",
      "validUntil",
    ]);
    expect(schemas.OperationsEvidenceMetadata.properties.accuracy.enum).toEqual([
      "exact",
      "sampled",
      "estimated",
      "unavailable",
    ]);
    expect(schemas.BackfillJob.properties.sourceMode.enum).toContain("tap_verified_resync");
    expect(schemas.BackfillJob.required).toContain("authorResults");
    expect(schemas.BackfillJob.required).toContain("verificationStatus");
    expect(schemas.BackfillJob.required).toContain("scopeTruncated");
    expect(schemas.BackfillAuthorResult.properties.status.enum).toEqual([
      "succeeded",
      "partial",
      "failed",
      "cancelled",
      "unsupported",
    ]);
    expect(schemas.IngestionGap.properties.status.enum).toContain("verification_required");
    expect(schemas.OperationsEnvironment.enum).toEqual(["dev", "prod"]);
    expect(schemas.OperationsCapabilities.required).toContain("recoveryModes");
    expect(schemas.DatabaseObservabilitySnapshot.required).not.toContain("cacheHitRatio");
    expect(schemas.OperationsOverviewEvidence.required).toEqual(["services", "ingestion", "database"]);
    expect(schemas.OperationsOverview.properties.evidence.$ref)
      .toBe("#/components/schemas/OperationsOverviewEvidence");
    expect(schemas.JetstreamDurabilityCheckpoint.properties.intakeHeartbeatAt).toMatchObject({
      type: ["string", "null"],
      format: "date-time",
    });
    expect(schemas.IngestionDurabilitySnapshot.required).toContain("inboxBySourceGeneration");
    expect(schemas.IngestionDurabilitySnapshot.properties.inboxBySourceGeneration).toEqual({
      type: "object",
      additionalProperties: { $ref: "#/components/schemas/IngestionInboxMetrics" },
    });
    expect(schemas.IngestionInboxMetrics.required).toEqual([
      "pending",
      "leased",
      "retrying",
      "applied",
      "filteredScope",
      "deadLetters",
      "total",
    ]);

    for (const schemaName of ["OperationsMutationRequest", "GapMutationRequest"]) {
      expect(schemas[schemaName].required).toContain("expectedVersion");
      expect(schemas[schemaName].required).toContain("idempotencyKey");
      expect(schemas[schemaName].required).not.toContain("auditNote");
    }
    expect(schemas.CreateBackfillRequest.required).toContain("requestFingerprint");

    expect(document.components.parameters.IdempotencyKey).toMatchObject({
      name: "Idempotency-Key",
      in: "header",
      required: true,
      schema: { type: "string", minLength: 1, maxLength: 128, pattern: "^\\S+$" },
    });

    const idempotentMutations: Array<[string, "post" | "patch"]> = [
      ["/v1/operations/ingestion/reconnect", "post"],
      ["/v1/operations/gaps/{id}", "patch"],
      ["/v1/operations/backfills", "post"],
      ["/v1/operations/backfills/{id}/pause", "post"],
      ["/v1/operations/backfills/{id}/resume", "post"],
      ["/v1/operations/backfills/{id}/cancel", "post"],
      ["/v1/operations/alerts/{id}/acknowledge", "post"],
      ["/v1/operations/alerts/{id}/resolve", "post"],
      ["/v1/operations/alerts/{id}/retry", "post"],
    ];
    for (const [path, method] of idempotentMutations) {
      expect(document.paths[path][method].parameters, `${method.toUpperCase()} ${path}`).toContainEqual({
        $ref: "#/components/parameters/IdempotencyKey",
      });
    }
    expect(document.paths["/v1/operations/backfills/{id}"].get.parameters).not.toContainEqual({
      $ref: "#/components/parameters/IdempotencyKey",
    });

    for (const path of [
      "/v1/operations/gaps/{id}",
      "/v1/operations/backfills/{id}/pause",
      "/v1/operations/backfills/{id}/resume",
      "/v1/operations/backfills/{id}/cancel",
      "/v1/operations/alerts/{id}/acknowledge",
      "/v1/operations/alerts/{id}/resolve",
      "/v1/operations/alerts/{id}/retry",
    ]) {
      const operation = document.paths[path]?.patch ?? document.paths[path]?.post;
      expect(operation.requestBody.content['application/json'].schema).toBeDefined();
      expect(operation.responses['409']).toBeDefined();
      expect(operation.responses['412']).toBeUndefined();
    }
  });

  it("uses named evidence-backed page wrappers and exact direct response shapes", () => {
    const document = Bun.YAML.parse(readFileSync(OPENAPI_PATH, "utf8")) as {
      paths: Record<string, Record<string, any>>;
      components: { schemas: Record<string, any> };
    };
    const schemas = document.components.schemas;
    const pages: Array<[string, string]> = [
      ["EndpointPage", "endpoints"],
      ["CommandPage", "commands"],
      ["GapPage", "gaps"],
      ["BackfillPage", "backfills"],
      ["AlertPage", "alerts"],
      ["TracePage", "traces"],
    ];
    for (const [schemaName, itemKey] of pages) {
      expect(schemas[schemaName].required, schemaName).toContain(itemKey);
      expect(schemas[schemaName].required, schemaName).toContain("evidence");
      expect(schemas[schemaName].properties[itemKey], schemaName).toBeDefined();
      expect(schemas[schemaName].properties.items, schemaName).toBeUndefined();
    }
    expect(schemas.TracePage.required).toContain("truncated");
    expect(schemas.OperationsServiceListResponse.required).toEqual(["services", "evidence"]);
    expect(schemas.OperationsIngestionResponse.required).toEqual(["sources", "evidence"]);
    expect(schemas.OperationsAppViewResponse.required).toEqual(["services", "evidence"]);
    expect(schemas.OperationsOverview.properties.viewerHistory).toMatchObject({
      type: "array",
      maxItems: 90,
      items: { $ref: "#/components/schemas/OperationsViewerCounts" },
    });
    expect(schemas.OperationsViewerCounts.required).toEqual([
      "knownViewers", "activeViewers7d", "activeViewers30d", "observedAt",
    ]);

    expect(document.paths["/v1/operations/services"].get.responses["200"].content["application/json"].schema.$ref)
      .toBe("#/components/schemas/OperationsServiceListResponse");
    expect(document.paths["/v1/operations/ingestion"].get.responses["200"].content["application/json"].schema.$ref)
      .toBe("#/components/schemas/OperationsIngestionResponse");
    expect(document.paths["/v1/operations/appview"].get.responses["200"].content["application/json"].schema.$ref)
      .toBe("#/components/schemas/OperationsAppViewResponse");
  });

  it("documents implemented lifecycle views, query contracts, and non-default success statuses", () => {
    const document = Bun.YAML.parse(readFileSync(OPENAPI_PATH, "utf8")) as {
      paths: Record<string, Record<string, any>>;
    };
    const paths = document.paths;
    const parameter = (path: string, name: string) =>
      paths[path].get.parameters.find((candidate: any) => candidate.name === name);

    expect(parameter("/v1/operations/gaps", "view").schema.enum).toEqual(["active", "history", "all"]);
    expect(parameter("/v1/operations/backfills", "view").schema.enum).toEqual([
      "active",
      "needs_attention",
      "history",
      "all",
    ]);
    expect(parameter("/v1/operations/alerts", "view").schema.enum).toEqual(["active", "history", "all"]);
    expect(parameter("/v1/operations/metrics", "resolution").schema.const).toBe("1m");
    const lastEventId = paths["/v1/operations/events/stream"].get.parameters
      .find((candidate: any) => candidate.name === "Last-Event-ID");
    expect(lastEventId.in).toBe("header");
    expect(lastEventId.schema).toMatchObject({ type: "integer", format: "int64", minimum: 0 });
    expect(parameter("/v1/operations/traces", "traceId")).toBeUndefined();
    expect(parameter("/v1/operations/traces", "from")).toBeDefined();
    expect(parameter("/v1/operations/traces", "to")).toBeDefined();

    expect(paths["/v1/operations/backfills"].post.responses["201"]).toBeDefined();
    expect(paths["/v1/operations/ingestion/reconnect"].post.responses["202"]).toBeDefined();
    expect(paths["/v1/operations/alerts/{id}/retry"].post.responses["202"]).toBeDefined();
    expect(paths["/v1/operations/events/stream"].get.responses["410"]).toBeDefined();
    for (const path of [
      "/v1/operations/gaps/{id}",
      "/v1/operations/gaps/{id}/investigation",
      "/v1/operations/backfills/{id}",
      "/v1/operations/backfills/{id}/pause",
      "/v1/operations/backfills/{id}/resume",
      "/v1/operations/backfills/{id}/cancel",
      "/v1/operations/alerts/{id}/acknowledge",
      "/v1/operations/alerts/{id}/resolve",
      "/v1/operations/alerts/{id}/retry",
      "/v1/operations/traces/{traceId}",
    ]) {
      const operation = paths[path].get ?? paths[path].post ?? paths[path].patch;
      expect(operation.responses["404"], path).toBeDefined();
    }
  });

  it("keeps Gateway pass-through behavior aligned with Operations statuses", () => {
    const proxy = readFileSync(OPERATIONS_PROXY_ROUTES, "utf8");
    // Accepted/created mutation statuses are forwarded unchanged, buffered or streamed.
    expect(proxy.match(/status: Self\.status\(Int\(reply\.status\.code\)\)/g)).toHaveLength(1);
    expect(proxy).toContain("let status = Self.status(Int(reply.status.code))");
    expect(proxy).toContain("guard (100...599).contains(code) else { return .badGateway }");
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
    const routes = readFileSync(join(GATEWAY_SOURCES, "LatrProxyRoutes.swift"), "utf8");
    expect(routes).toContain('group.patch("/xrpc/link.latr.bookmarks.setState")');
    expect(routes).toContain('group.post("/xrpc/link.latr.bookmarks.setState")');
    const bruno = readFileSync(join(import.meta.dir, "../../../services/gateway/bruno/Latr/Set Bookmark State PATCH Compatibility.bru"), "utf8");
    expect(bruno).toContain("patch {");
    expect(bruno).toContain("X-Latr-Gateway-DPoP:");
  });
});

describe("Selectable Finance feed contracts", () => {
  it("keeps feed context and catalog definitions aligned with the lexicons", () => {
    const document = Bun.YAML.parse(readFileSync(OPENAPI_PATH, "utf8")) as any;
    const lexiconRoot = join(import.meta.dir, "../../lexicons/app/thesocialwire/discovery");
    const query = JSON.parse(readFileSync(join(lexiconRoot, "getFinance.json"), "utf8"));
    const defs = JSON.parse(readFileSync(join(lexiconRoot, "defs.json"), "utf8")).defs;
    const feed = document.paths["/xrpc/app.thesocialwire.discovery.getFinance"].get.parameters.find((p: any) => p.name === "feed");
    expect(feed.schema).toEqual(query.defs.main.parameters.properties.feed && {type:"string",maxLength:256,default:"finance"});
    expect(document.components.schemas.FinancePage.required).toContain("feedId");
    expect(defs.financePage.required).toContain("feedId");
    expect(document.components.schemas.FinanceAvailability.required).toContain("feeds");
    expect(document.components.schemas.FinanceFeedDefinition.required).toEqual(defs.financeFeedDefinition.required);
    expect(document.components.schemas.FinanceFeedDefinition.properties.kind.enum).toEqual(defs.financeFeedDefinition.properties.kind.enum);
    for (const service of ["gateway", "appview"]) {
      const request = readFileSync(join(import.meta.dir, `../../../services/${service}/bruno/XRPC/app.thesocialwire.discovery.getFinance.bru`), "utf8");
      expect(request).toContain("?feed=finance&");
      expect(request).toContain("group:mag7");
    }
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
    for (const service of ["gateway", "appview"]) {
      for (const name of ["Get Lists", "Search Creator Lists", "Resolve List", "Refresh Lists"]) {
        expect(readFileSync(join(import.meta.dir, `../../../services/${service}/bruno/Lists/${name}.bru`), "utf8")).toContain("DPoP:");
      }
    }
  });
});
