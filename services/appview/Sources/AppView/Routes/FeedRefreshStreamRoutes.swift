import Foundation
import GatewayCore
import Hummingbird
import ThinAppViewCore

struct FeedRefreshStreamRoutes {
  let feedRefreshStreamService: FeedRefreshStreamService

  struct Input: Decodable {
    let kind: String
    let id: String?
  }

  func register(on group: RouterGroup<GatewayRequestContext>) {
    group.post("/xrpc/app.thesocialwire.appview.refreshFeed") { request, context async throws -> Response in
      guard let auth = context.authContext else { throw HTTPError(.unauthorized) }
      let input = try await request.decode(as: Input.self, context: context)
      guard let kind = AppViewFeedKind(rawValue: input.kind) else {
        throw ThinAppViewRoutes.invalidRequest("Input requires a valid `kind`", requestId: context.requestId)
      }
      let selector = AppViewFeedSelector(kind: kind, id: input.id)
      if kind.requiresId, selector.id.isEmpty {
        throw ThinAppViewRoutes.invalidRequest("Input requires `id` for this feed kind", requestId: context.requestId)
      }
      var headers = HTTPFields()
      headers[.contentType] = "application/x-ndjson"
      headers[.cacheControl] = "no-cache"
      return Response(
        status: .ok,
        headers: headers,
        body: ResponseBody { writer in
          try await feedRefreshStreamService.writeStream(auth: auth, selector: selector, writer: &writer)
        }
      )
    }
  }
}
