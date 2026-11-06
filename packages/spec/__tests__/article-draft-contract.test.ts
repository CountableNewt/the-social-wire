import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dir, "../../..");
const spec = Bun.YAML.parse(readFileSync(join(root, "packages/spec/openapi.yaml"), "utf8")) as any;
const manifest = JSON.parse(readFileSync(join(root, "packages/spec/endpoint-manifest.json"), "utf8"));
const prefix = "/xrpc/app.thesocialwire.articleDraft.";

test("private article methods have owner-bound auth, no public cache, and matching Bruno requests", () => {
  expect(Object.keys(spec.paths).filter(path => path.startsWith(prefix))).toHaveLength(4);
  for (const name of ["list", "get", "put", "delete"]) {
    const method = ["list", "get"].includes(name) ? "get" : "post";
    const operation = spec.paths[prefix + name][method];
    expect(operation.security).toEqual([{ ATProtoOAuthDPoP: [] }]);
    expect(operation.description).toContain("Never publishes drafts or images to the PDS");
    expect(operation.responses["200"].headers["Cache-Control"].schema.const).toBe("private, no-store");
    expect(operation.responses["200"].headers["X-Content-Type-Options"].schema.const).toBe("nosniff");
    expect(operation.responses["409"].description).toContain("Conflict");
    expect(operation.responses["503"].description).toBe("Unavailable");
    expect(manifest.entries).toContainEqual({ surface: "openapi", method: method.toUpperCase(), path: prefix + name, classification: "xrpc-migration", xrpcNsid: `app.thesocialwire.articleDraft.${name}` });
    for (const service of ["gateway", "appview"]) {
      const request = readFileSync(join(root, `services/${service}/bruno/XRPC/app.thesocialwire.articleDraft.${name}.bru`), "utf8");
      expect(request).toContain(`${method} {`);
      expect(request).toContain(prefix + name);
      expect(request).toContain("DPoP:");
      expect(request).toContain("Never writes draft content or images to the PDS");
    }
  }
});

test("CAS, idempotency, bounded discovery, and tombstones are part of the canonical contract", () => {
  const schemas = spec.components.schemas;
  expect(schemas.ArticleDraftPutInput.required).toEqual(["expectedRevision", "mutationId", "draft", "attachments"]);
  expect(schemas.ArticleDraftDeleteInput.required).toEqual(["draftId", "expectedRevision", "mutationId"]);
  expect(schemas.ArticleDraftPutInput.properties.expectedRevision.minimum).toBe(0);
  expect(schemas.ArticleDraftPutInput.properties.attachments.maxItems).toBe(200);
  expect(schemas.ArticleDraftPayload.properties.assets.maxItems).toBe(200);
  expect(schemas.ArticleDraftPayload.properties.publicationClaimId.format).toBe("uuid");
  expect(schemas.ArticleDraftDocument.properties.revision.minimum).toBe(1);
  expect(schemas.ArticleDraftDocument.allOf[1].then).toEqual({ not: { required: ["draft"] }, properties: { attachments: { maxItems: 0 } } });
  expect(spec.paths[prefix + "list"].get.parameters.find((parameter: any) => parameter.name === "limit").schema).toEqual({ type: "integer", minimum: 1, maximum: 20, default: 20 });
  expect(spec.paths[prefix + "list"].get.description).toContain("ascending keyset");
  expect(spec.paths[prefix + "put"].post.requestBody.description).toContain("512 KiB");
  expect(schemas.ArticleDraftAttachmentBinding.properties.asset.$ref).toBe("#/components/schemas/SocialDraftAsset");
});
