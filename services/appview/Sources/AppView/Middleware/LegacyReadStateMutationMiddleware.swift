import GatewayCore
import Hummingbird
import ThinAppViewCore

/// An authority transition can finish between the initial check and the locked write.
struct LegacyReadStateMutationMiddleware: RouterMiddleware {
  typealias Context = GatewayRequestContext

  func handle(_ request: Request, context: GatewayRequestContext,
    next: (Request, GatewayRequestContext) async throws -> Response
  ) async throws -> Response {
    do {
      return try await next(request, context)
    } catch PDSReadStateStorageError.alreadyMigrated {
      return try AppViewFeedError(status: .conflict, code: "PDSReadStateRequired",
        message: "Update your client to change PDS read state.", requestId: context.requestId,
        retryable: false).response(from: request, context: context)
    }
  }
}
