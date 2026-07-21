import { afterEach, expect, test } from "bun:test"
import { operationsEnvironment } from "@/lib/app-environment"
import { gatewayOrigin, operationsRequest } from "@/lib/operations-api"
import type { OAuthSession } from "@/lib/auth"

const originalAppEnv = process.env.NEXT_PUBLIC_APP_ENV
const originalGatewayOrigin = process.env.NEXT_PUBLIC_OPERATIONS_GATEWAY_ORIGIN
afterEach(() => {
  restoreEnvironment("NEXT_PUBLIC_APP_ENV", originalAppEnv)
  restoreEnvironment("NEXT_PUBLIC_OPERATIONS_GATEWAY_ORIGIN", originalGatewayOrigin)
})

test("uses the deployment's single configured gateway origin", () => {
  process.env.NEXT_PUBLIC_OPERATIONS_GATEWAY_ORIGIN = "https://operations-gateway.example"
  expect(gatewayOrigin()).toBe("https://operations-gateway.example")
})

test("derives the fixed operations environment from APP_ENV", () => {
  process.env.NEXT_PUBLIC_APP_ENV = "prod"
  expect(operationsEnvironment()).toBe("production")

  process.env.NEXT_PUBLIC_APP_ENV = "dev"
  expect(operationsEnvironment()).toBe("development")
})

test("defaults the gateway origin from the fixed environment", () => {
  delete process.env.NEXT_PUBLIC_OPERATIONS_GATEWAY_ORIGIN
  process.env.NEXT_PUBLIC_APP_ENV = "prod"
  expect(gatewayOrigin()).toBe("https://api.thesocialwire.app")

  process.env.NEXT_PUBLIC_APP_ENV = "dev"
  expect(gatewayOrigin()).toBe("https://api.testing.thesocialwire.app")
})

test("operations requests propagate request and W3C trace identifiers", async () => {
  let headers = new Headers()
  const session = {
    fetchHandler: async (_url: string, init?: RequestInit) => {
      headers = new Headers(init?.headers)
      return new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      })
    },
  } as unknown as OAuthSession

  await operationsRequest(session, "/v1/operations/overview")

  expect(headers.get("X-Request-ID")).toBeTruthy()
  expect(headers.get("traceparent")).toMatch(/^00-[0-9a-f]{32}-[0-9a-f]{16}-01$/)
})

function restoreEnvironment(key: string, value: string | undefined) {
  if (value === undefined) delete process.env[key]
  else process.env[key] = value
}
