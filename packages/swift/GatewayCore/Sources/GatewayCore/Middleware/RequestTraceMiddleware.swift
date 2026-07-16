import Foundation
import HTTPTypes
import Hummingbird
import OperationsCore

public struct RequestTraceMiddleware: RouterMiddleware {
  public typealias Context = GatewayRequestContext

  public init() {}

  public func handle(
    _ request: Request,
    context: GatewayRequestContext,
    next: (Request, GatewayRequestContext) async throws -> Response
  ) async throws -> Response {
    let requestHeader = HTTPField.Name("X-Request-ID")
    let traceHeader = HTTPField.Name("traceparent")
    var mutableContext = context
    mutableContext.requestId = requestHeader.flatMap { request.headers[$0] }
      .flatMap(Self.safeRequestId) ?? UUID().uuidString.lowercased()

    let incoming = traceHeader.flatMap { request.headers[$0] }.flatMap(TraceContext.init(traceparent:))
    let routeSampleRate = request.uri.path.contains("bootstrap-stream") ? 10 : 5
    mutableContext.traceContext = incoming?.child()
      ?? TraceContext(sampled: Int.random(in: 0..<100) < routeSampleRate)

    var response = try await next(request, mutableContext)
    if let requestHeader { response.headers[requestHeader] = mutableContext.requestId }
    if let traceHeader { response.headers[traceHeader] = mutableContext.traceContext.traceparent }
    return response
  }

  private static func safeRequestId(_ value: String) -> String? {
    let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !trimmed.isEmpty, trimmed.count <= 128,
          trimmed.unicodeScalars.allSatisfy({ $0.isASCII && !$0.properties.isWhitespace })
    else { return nil }
    return trimmed
  }
}
