import AsyncHTTPClient
import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import Logging
import Testing
@testable import Gateway

struct SubscriptionMigrationTests {
  @Test func entitlementsAreBoundToTheAuthenticatedViewerAndNeverRequestParameters() async throws {
    let router = Router(context: GatewayRequestContext.self)
    router.add(middleware: Viewer())
    SubscriptionRoutes(entitlements: GatewayEntitlements(gatingEnabled: true, proDIDs: ["did:plc:other"])).register(on: router.group())
    try await Application(router: router).test(.router) { client in
      let response = try await client.execute(uri: "/v1/subscription/entitlements?viewerDid=did:plc:other&plan=pro", method: .get)
      let body = try #require(JSONSerialization.jsonObject(with: Data(response.body.readableBytesView)) as? [String: String])
      #expect(body == ["viewerDid": "did:plc:viewer", "plan": "free"])
      #expect(response.headers[.cacheControl] == "private, no-store")
    }
  }

  @Test func analyticsProGateRejectsBeforeUpstreamAndEventsStillRequireIdentity() async throws {
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    do {
      let router = Router(context: GatewayRequestContext.self)
      router.add(middleware: Viewer())
      AnalyticsProxyRoutes(proxy: AppViewProxyRoutes(baseURL: "http://must-not-be-called.invalid", internalSecret: nil, httpClient: http, logger: Logger(label: "analytics-test")), entitlements: GatewayEntitlements(gatingEnabled: true, proDIDs: [])).register(on: router.group())
      try await Application(router: router).test(.router) { client in
        let bluesky = try await client.execute(uri: "/v1/analytics/bluesky", method: .get)
        #expect(bluesky.status == .forbidden)
        #expect(bluesky.headers[.cacheControl] == "private, no-store")
        let publications = try await client.execute(uri: "/v1/analytics/publications", method: .get)
        #expect(publications.status == .forbidden)
        #expect(publications.headers[.cacheControl] == "private, no-store")
        let response = try await client.execute(uri: "/v1/analytics/summary", method: .get)
        #expect(response.status == .forbidden)
        #expect(response.headers[.cacheControl] == "private, no-store")
        #expect(String(buffer: response.body).contains("ProRequired"))
      }
    } catch { try? await http.shutdown(); throw error }
    try await http.shutdown()
  }

  @Test func toolsRemainOffInProductionUnlessExplicitlyEnabled() throws {
    let env = ["APP_ENV": "prod", "DATABASE_URL": "postgres://test/db", "PDS_ATTESTATION_RECEIPT_SECRET": String(repeating: "s", count: 32)]
    #expect(try !GatewayServiceConfig.fromEnvironment(env).toolsEnabled)
    #expect(try GatewayServiceConfig.fromEnvironment(env.merging(["TOOLS_ENABLED": " TRUE "]) { _, new in new }).toolsEnabled)
    #expect(try GatewayServiceConfig.fromEnvironment(["APP_ENV": "dev", "DATABASE_URL": "postgres://test/db", "PDS_ATTESTATION_RECEIPT_SECRET": String(repeating: "s", count: 32)]).toolsEnabled)
    #expect(!GatewayEntitlements(gatingEnabled: true, proDIDs: [GatewayInternalTrustAuthMiddleware.anonymousDiscoveryDID]).hasPro(did: GatewayInternalTrustAuthMiddleware.anonymousDiscoveryDID))
  }

  private struct Viewer: RouterMiddleware {
    func handle(_ request: Request, context: GatewayRequestContext, next: (Request, GatewayRequestContext) async throws -> Response) async throws -> Response {
      var context = context
      context.authContext = AuthContext(did: "did:plc:viewer", authorizationForwardingValue: "DPoP fixture", dpopProof: "fixture")
      return try await next(request, context)
    }
  }
}
