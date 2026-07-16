import Foundation
import Hummingbird
import OperationsCore

/// Custom request context that carries per-request auth state through the middleware chain.
///
/// Hummingbird 2 uses a typed context instead of a stringly-keyed storage bag.
/// `ATProtoAuthMiddleware` sets `authContext` after verifying the access JWT (`Bearer`/`DPoP` Authorization prefix) alongside the RFC 9449 **`DPoP`** header binding;
/// route handlers read it to obtain the caller's DID.
public struct GatewayRequestContext: RequestContext {
  public var coreContext: CoreRequestContextStorage

  /// Injected by `ATProtoAuthMiddleware` after successful token verification.
  /// `nil` on unauthenticated routes (e.g. `/health`).
  public var authContext: AuthContext?
  public var requestId: String
  public var traceContext: TraceContext

  public init(source: Source) {
    self.coreContext = .init(source: source)
    self.authContext = nil
    self.requestId = UUID().uuidString.lowercased()
    self.traceContext = TraceContext(sampled: false)
  }
}
