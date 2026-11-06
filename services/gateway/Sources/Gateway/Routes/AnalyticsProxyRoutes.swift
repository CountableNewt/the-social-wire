import Foundation
import GatewayCore
import Hummingbird
import NIOCore

struct AnalyticsProxyRoutes {
  let proxy: AppViewProxyRoutes
  let entitlements: GatewayEntitlements

  func register(on group: RouterGroup<GatewayRequestContext>) {
    group.get("/v1/analytics/summary") { request, context in
      try await forward(request, context: context, path: "/v1/analytics/summary", method: "GET", requiresPro: true)
    }
    for path in ["/v1/analytics/refresh", "/v1/analytics/events"] {
      group.post(RouterPath(path)) { request, context in
        try await forward(request, context: context, path: path, method: "POST", requiresPro: path.hasSuffix("refresh"))
      }
    }
  }

  private func forward(_ request: Request, context: GatewayRequestContext, path: String, method: String, requiresPro: Bool) async throws -> Response {
    guard let auth = context.authContext, !auth.did.isEmpty,
      auth.did != GatewayInternalTrustAuthMiddleware.anonymousDiscoveryDID
    else { return Response(status: .unauthorized, headers: [.cacheControl: "private, no-store"]) }
    if requiresPro && entitlements.gatingEnabled && !entitlements.hasPro(did: auth.did) {
      let data = try JSONSerialization.data(withJSONObject: ["error": "ProRequired", "message": "A Pro subscription is required"])
      return Response(status: .forbidden, headers: [.contentType: "application/json", .cacheControl: "private, no-store"], body: .init(byteBuffer: ByteBuffer(bytes: data)))
    }
    return try await proxy.forward(request: request, context: context, path: path, method: method)
  }
}
