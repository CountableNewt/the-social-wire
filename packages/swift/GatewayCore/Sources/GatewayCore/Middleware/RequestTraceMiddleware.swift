import Foundation
import HTTPTypes
import Hummingbird
import OperationsCore

public struct RequestTraceMiddleware: RouterMiddleware {
  public typealias Context = GatewayRequestContext

  private let service: String
  private let environment: String
  private let instanceId: String
  private let telemetry: OperationsTelemetryBuffer?

  public init(
    service: String = "unknown",
    environment: String = "unknown",
    instanceId: String = "unknown",
    telemetry: OperationsTelemetryBuffer? = nil
  ) {
    self.service = service
    self.environment = environment
    self.instanceId = instanceId
    self.telemetry = telemetry
  }

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

    let startedAt = Date()
    do {
      var response = try await next(request, mutableContext)
      let duration = Date().timeIntervalSince(startedAt)
      await record(request: request, context: mutableContext, startedAt: startedAt, duration: duration, statusCode: response.status.code, errorType: nil)
      if let requestHeader { response.headers[requestHeader] = mutableContext.requestId }
      if let traceHeader { response.headers[traceHeader] = mutableContext.traceContext.traceparent }
      return response
    } catch {
      await record(request: request, context: mutableContext, startedAt: startedAt, duration: Date().timeIntervalSince(startedAt), statusCode: 500, errorType: OperationsRedactor.errorCategory(error))
      throw error
    }
  }

  private static func safeRequestId(_ value: String) -> String? {
    let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !trimmed.isEmpty, trimmed.count <= 128,
          trimmed.unicodeScalars.allSatisfy({ $0.isASCII && !$0.properties.isWhitespace })
    else { return nil }
    return trimmed
  }

  private func record(
    request: Request,
    context: GatewayRequestContext,
    startedAt: Date,
    duration: TimeInterval,
    statusCode: Int,
    errorType: String?
  ) async {
    guard let telemetry else { return }
    let route = Self.routeTemplate(request.uri.path)
    let statusClass = "\(statusCode / 100)xx"
    let dimensions = [
      "service": service,
      "environment": environment,
      "route_template": route,
      "method": request.method.rawValue,
      "status_class": statusClass,
    ]
    _ = await telemetry.enqueue(.metric(.init(name: "socialwire.http.server.requests_total", value: 1, dimensions: dimensions)))
    _ = await telemetry.enqueue(.metric(.init(name: "socialwire.http.server.duration_seconds", value: duration, dimensions: dimensions)))
    let isError = statusCode >= 500 || errorType != nil
    if context.traceContext.sampled || isError {
      var attributes = dimensions
      if let errorType { attributes["error_type"] = errorType }
      _ = await telemetry.enqueue(.span(.init(
        traceId: context.traceContext.traceId,
        parentSpanId: nil,
        service: service,
        name: "\(service).request",
        startedAt: startedAt,
        durationMs: duration * 1_000,
        status: isError ? "error" : "ok",
        attributes: attributes,
        expiresAt: startedAt.addingTimeInterval(isError ? 30 * 86_400 : 7 * 86_400)
      )))
    }
    _ = await telemetry.enqueue(.event(.init(
      service: service,
      environment: environment,
      instanceId: instanceId,
      name: service == "appview" ? "appview.request.completed" : "\(service).request.completed",
      requestId: context.requestId,
      traceId: context.traceContext.traceId,
      attributes: dimensions
    )))
  }

  private static func routeTemplate(_ path: String) -> String {
    var components = path.split(separator: "/").map(String.init)
    guard components.count >= 4, components[0] == "v1", components[1] == "operations" else {
      return String(path.prefix(160))
    }
    if ["gaps", "backfills", "alerts", "traces"].contains(components[2]) {
      components[3] = ":id"
    }
    return "/" + components.joined(separator: "/")
  }
}
