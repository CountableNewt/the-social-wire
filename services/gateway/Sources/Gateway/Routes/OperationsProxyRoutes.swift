import AsyncHTTPClient
import Foundation
import GatewayCore
import HTTPTypes
import Hummingbird
import NIOCore

/// Proxies the operator control plane with a secret distinct from Gateway → AppView trust.
struct OperationsProxyRoutes {
  let baseURL: String
  let internalSecret: String?
  let httpClient: HTTPClient

  func register(on group: RouterGroup<GatewayRequestContext>) {
    get("/v1/operations/overview", path: "/v1/operations/overview", on: group)
    get("/v1/operations/services", path: "/v1/operations/services", on: group)
    get("/v1/operations/ingestion", path: "/v1/operations/ingestion", on: group)
    get("/v1/operations/appview", path: "/v1/operations/appview", on: group)
    get("/v1/operations/gaps", path: "/v1/operations/gaps", on: group)
    group.get("/v1/operations/gaps/:id/investigation") { request, context async throws -> Response in
      guard let id = context.parameters.get("id") else { throw HTTPError(.badRequest) }
      return try await forward(
        request,
        context: context,
        path: "/v1/operations/gaps/\(id)/investigation",
        method: "GET"
      )
    }
    group.patch("/v1/operations/gaps/:id") { request, context async throws -> Response in
      guard let id = context.parameters.get("id") else { throw HTTPError(.badRequest) }
      return try await forward(request, context: context, path: "/v1/operations/gaps/\(id)", method: "PATCH")
    }
    get("/v1/operations/backfills", path: "/v1/operations/backfills", on: group)
    post("/v1/operations/backfills/dry-run", path: "/v1/operations/backfills/dry-run", on: group)
    post("/v1/operations/backfills", path: "/v1/operations/backfills", on: group)
    group.get("/v1/operations/backfills/:id") { request, context async throws -> Response in
      guard let id = context.parameters.get("id") else { throw HTTPError(.badRequest) }
      return try await forward(request, context: context, path: "/v1/operations/backfills/\(id)", method: "GET")
    }
    for action in ["pause", "resume", "cancel"] {
      group.post("/v1/operations/backfills/:id/\(action)") { request, context async throws -> Response in
        guard let id = context.parameters.get("id") else { throw HTTPError(.badRequest) }
        return try await forward(
          request,
          context: context,
          path: "/v1/operations/backfills/\(id)/\(action)",
          method: "POST"
        )
      }
    }
    get("/v1/operations/alerts", path: "/v1/operations/alerts", on: group)
    for action in ["acknowledge", "resolve"] {
      group.post("/v1/operations/alerts/:id/\(action)") { request, context async throws -> Response in
        guard let id = context.parameters.get("id") else { throw HTTPError(.badRequest) }
        return try await forward(
          request,
          context: context,
          path: "/v1/operations/alerts/\(id)/\(action)",
          method: "POST"
        )
      }
    }
    get("/v1/operations/traces", path: "/v1/operations/traces", on: group)
    group.get("/v1/operations/traces/:traceId") { request, context async throws -> Response in
      guard let traceId = context.parameters.get("traceId") else { throw HTTPError(.badRequest) }
      return try await forward(
        request,
        context: context,
        path: "/v1/operations/traces/\(traceId)",
        method: "GET"
      )
    }
  }

  private func get(_ route: RouterPath, path: String, on group: RouterGroup<GatewayRequestContext>) {
    group.get(route) { request, context async throws -> Response in
      try await forward(request, context: context, path: path, method: "GET")
    }
  }

  private func post(_ route: RouterPath, path: String, on group: RouterGroup<GatewayRequestContext>) {
    group.post(route) { request, context async throws -> Response in
      try await forward(request, context: context, path: path, method: "POST")
    }
  }

  private func forward(
    _ request: Request,
    context: GatewayRequestContext,
    path: String,
    method: String
  ) async throws -> Response {
    guard let auth = context.authContext else { throw HTTPError(.unauthorized) }
    let pathWithQuery = GatewayInternalTrust.canonicalPathWithQuery(path: path, query: request.uri.query)
    var outbound = HTTPClientRequest(url: "\(normalizedBase)\(pathWithQuery)")
    outbound.method = method == "GET" ? .GET : method == "PATCH" ? .PATCH : .POST
    outbound.headers.add(name: "Accept", value: "application/json")
    outbound.headers.add(name: "Authorization", value: auth.authorizationForwardingValue)
    outbound.headers.add(name: "X-Request-ID", value: context.requestId)
    outbound.headers.add(name: "traceparent", value: context.traceContext.traceparent)
    if let dpop = auth.dpopProof { outbound.headers.add(name: "DPoP", value: dpop) }
    if let internalSecret {
      let signed = try GatewayInternalTrust.signedHeaders(
        secret: internalSecret,
        did: auth.did,
        method: method,
        pathWithQuery: GatewayInternalTrust.canonicalSignedPath(path)
      )
      for header in signed { outbound.headers.add(name: header.name, value: header.value) }
    }
    if method != "GET" {
      let body = try await request.body.collect(upTo: 2 * 1024 * 1024)
      if body.readableBytes > 0 {
        outbound.body = .bytes(body)
        outbound.headers.add(name: "Content-Type", value: "application/json")
      }
    }
    let reply = try await httpClient.execute(outbound, timeout: .seconds(60))
    let body = try await reply.body.collect(upTo: 8 * 1024 * 1024)
    var headers = HTTPFields()
    headers[.contentType] = reply.headers["content-type"].first ?? "application/json"
    let status = Self.status(Int(reply.status.code))
    return Response(status: status, headers: headers, body: .init(byteBuffer: body))
  }

  private var normalizedBase: String {
    var value = baseURL.trimmingCharacters(in: .whitespacesAndNewlines)
    while value.hasSuffix("/") { value.removeLast() }
    return value
  }

  private static func status(_ code: Int) -> HTTPResponse.Status {
    switch code {
    case 200: .ok
    case 201: .created
    case 204: .noContent
    case 400: .badRequest
    case 401: .unauthorized
    case 403: .forbidden
    case 404: .notFound
    case 409: .conflict
    case 422: .unprocessableContent
    case 503: .serviceUnavailable
    default: code >= 500 ? .badGateway : .internalServerError
    }
  }
}
