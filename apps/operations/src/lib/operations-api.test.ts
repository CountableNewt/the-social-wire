import { afterEach, expect, test } from "bun:test"
import { gatewayOrigin, operationsRequest } from "@/lib/operations-api"
import type { OAuthSession } from "@/lib/auth"

const originalDev = process.env.NEXT_PUBLIC_OPERATIONS_DEV_GATEWAY_ORIGIN
const originalProd = process.env.NEXT_PUBLIC_OPERATIONS_PROD_GATEWAY_ORIGIN
afterEach(() => { process.env.NEXT_PUBLIC_OPERATIONS_DEV_GATEWAY_ORIGIN = originalDev; process.env.NEXT_PUBLIC_OPERATIONS_PROD_GATEWAY_ORIGIN = originalProd })

test("environment switching maps to separate configured gateway origins", () => {
  process.env.NEXT_PUBLIC_OPERATIONS_DEV_GATEWAY_ORIGIN = "https://dev.example"
  process.env.NEXT_PUBLIC_OPERATIONS_PROD_GATEWAY_ORIGIN = "https://prod.example"
  expect(gatewayOrigin("development")).toBe("https://dev.example")
  expect(gatewayOrigin("production")).toBe("https://prod.example")
})

test("operations requests propagate request and W3C trace identifiers", async () => {
  let headers = new Headers()
  const session = {
    fetchHandler: async (_url: string, init?: RequestInit) => {
      headers = new Headers(init?.headers)
      return new Response(JSON.stringify({ ok: true }), { status: 200, headers: { "Content-Type": "application/json" } })
    },
  } as unknown as OAuthSession

  await operationsRequest(session, "development", "/v1/operations/overview")

  expect(headers.get("X-Request-ID")).toBeTruthy()
  expect(headers.get("traceparent")).toMatch(/^00-[0-9a-f]{32}-[0-9a-f]{16}-01$/)
})
