import Foundation
import GatewayCore
import Hummingbird
import NIOCore

struct SubscriptionRoutes {
  let entitlements: GatewayEntitlements

  func register(on group: RouterGroup<GatewayRequestContext>) {
    group.get("/v1/subscription/entitlements") { _, context -> Response in
      guard let auth = context.authContext, !auth.did.isEmpty,
        auth.did != GatewayInternalTrustAuthMiddleware.anonymousDiscoveryDID
      else { return Response(status: .unauthorized, headers: [.cacheControl: "private, no-store"]) }
      let data = try JSONSerialization.data(withJSONObject: ["viewerDid": auth.did, "plan": entitlements.hasPro(did: auth.did) ? "pro" : "free"])
      return Response(status: .ok, headers: [.contentType: "application/json", .cacheControl: "private, no-store"], body: .init(byteBuffer: ByteBuffer(bytes: data)))
    }
  }
}
