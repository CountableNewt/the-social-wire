import { expect, test } from "bun:test"
import { buildOperationsOAuthClientMetadata } from "@/lib/operations-oauth-client-metadata"

test("builds same-origin metadata for hosted deployments", () => {
  const metadata = buildOperationsOAuthClientMetadata("https://preview.example.com/some-path")

  expect(metadata.client_id).toBe("https://preview.example.com/operations-client-metadata.json")
  expect(metadata.redirect_uris).toEqual(["https://preview.example.com/callback"])
  expect(metadata.client_uri).toBe("https://preview.example.com")
  expect(metadata.scope).toBe("atproto")
})
