import Foundation
import GatewayCore
import Hummingbird
import NIOFoundationCompat

struct MediaProjectRoutes: Sendable {
  func register(on group: RouterGroup<GatewayRequestContext>) {
    group.post("/v1/media/project") { request, context in
      guard let did = context.authContext?.did, !did.isEmpty,
        did != GatewayInternalTrustAuthMiddleware.anonymousDiscoveryDID
      else { return try Self.response(["error": "Authentication Required"], status: .unauthorized) }
      do {
        let body = try await request.body.collect(upTo: MediaProjectRules.maximumBytes)
        return try Self.response(MediaProjectRules.project(Data(buffer: body)))
      } catch {
        return try Self.response(["error": "Invalid Media Candidates"], status: .badRequest)
      }
    }
  }

  private static func response(_ value: [String: Any], status: HTTPResponse.Status = .ok) throws
    -> Response
  {
    Response(
      status: status,
      headers: [.contentType: "application/json", .cacheControl: "private, no-store"],
      body: .init(byteBuffer: .init(data: try JSONSerialization.data(withJSONObject: value))))
  }
}
