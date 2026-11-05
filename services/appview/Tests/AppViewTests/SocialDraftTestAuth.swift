import GatewayCore
import Hummingbird

struct SocialDraftTestAuth: RouterMiddleware {
  typealias Context = GatewayRequestContext
  let owner: String?
  func handle(_ request: Request, context: GatewayRequestContext,
    next: (Request, GatewayRequestContext) async throws -> Response
  ) async throws -> Response {
    var context = context
    if let owner { context.authContext = AuthContext(did: owner, authorizationForwardingValue: "fixture", dpopProof: "fixture") }
    return try await next(request, context)
  }
}
