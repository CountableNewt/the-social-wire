import GatewayCore
import Hummingbird
import OperationsCore

struct OperatorAuthorizationMiddleware: RouterMiddleware {
  typealias Context = GatewayRequestContext

  let allowedDids: Set<String>

  func handle(
    _ request: Request,
    context: GatewayRequestContext,
    next: (Request, GatewayRequestContext) async throws -> Response
  ) async throws -> Response {
    guard let did = context.authContext?.did else { throw HTTPError(.unauthorized) }
    guard allowedDids.contains(did) else {
      throw HTTPError(.forbidden, message: "Authenticated DID is not an operations operator")
    }
    return try await next(request, context)
  }
}
