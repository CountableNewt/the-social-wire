import Foundation
import GatewayCore
import Hummingbird
import HTTPTypes
import NIOFoundationCompat

struct AnalyticsRoutes: Sendable {
  let store: AnalyticsStore
  let provider: any AnalyticsContentProviding
  let limits = AnalyticsLimits()
  var now: @Sendable () -> Date = { Date() }

  func register(on group: RouterGroup<GatewayRequestContext>) {
    group.get("/v1/analytics/summary") { request, context in try await handle(request, context: context) }
    group.post("/v1/analytics/refresh") { request, context in try await handle(request, context: context) }
    group.post("/v1/analytics/events") { request, context in try await handle(request, context: context) }
  }

  func handle(_ request: Request, context: GatewayRequestContext) async throws -> Response {
    let deadline = SocialDraftDeadline(seconds: 20, closeTransport: context.closeTransport)
    let work = Task { try await operation(request, context: context) }
    deadline.bind { work.cancel() }
    let response: Response
    do {
      response = try await withTaskCancellationHandler { try await work.value } onCancel: { work.cancel(); context.closeTransport() }
    } catch { response = try Self.failure(.serviceUnavailable, "Analytics Unavailable") }
    var result = response
    let body = response.body
    result.body = ResponseBody(contentLength: body.contentLength) { writer in
      defer { deadline.complete() }
      try await body.write(writer)
    }
    result.headers[.contentType] = "application/json"
    result.headers[.cacheControl] = "no-store"
    return result
  }

  private func operation(_ request: Request, context: GatewayRequestContext) async throws -> Response {
    guard let author = context.authContext?.did, !author.isEmpty,
      author != GatewayInternalTrustAuthMiddleware.anonymousDiscoveryDID else { return try Self.failure(.unauthorized, "Authentication Required") }
    let at = now()
    let path = request.uri.path
    guard await limits.allow(key: author + "\0" + path, at: at, maximum: path == "/v1/analytics/refresh" ? 1 : 120) else {
      return try Self.failure(.tooManyRequests, "Analytics Request Limit Reached")
    }
    if path == "/v1/analytics/events" {
      let event: AnalyticsEvent
      do {
        let body = try await request.body.collect(upTo: 4096)
        let data = Data(buffer: body)
        guard let object = try JSONSerialization.jsonObject(with: data) as? [String: Any],
          Set(object.keys) == Set(["subjectUri", "event", "surface"]) else { throw AnalyticsError.invalid }
        event = try JSONDecoder().decode(AnalyticsEvent.self, from: data)
        guard ["impression", "view", "outbound_click"].contains(event.event),
          ["social-feed", "article-feed", "post-viewer", "article-reader", "messages"].contains(event.surface) else { throw AnalyticsError.invalid }
      } catch { return try Self.failure(.badRequest, "Invalid Analytics Observation") }
      guard let subject = try? AnalyticsSubject(event.subjectUri) else { return try Self.failure(.badRequest, "Invalid Subject") }
      if subject.did == author { return Response(status: .accepted) }
      do { try await provider.validate(event.subjectUri) }
      catch { return try Self.failure(.unprocessableContent, "Canonical Content Unavailable") }
      do { try await store.record(viewer: author, author: subject.did, event: event, at: at) }
      catch AnalyticsError.rateLimit { return try Self.failure(.tooManyRequests, "Analytics Observation Limit Reached") }
      catch { return try Self.failure(.serviceUnavailable, "Analytics Unavailable") }
      return Response(status: .accepted)
    }
    let query = request.uri.queryParameters
    let days = query.get("days").flatMap(Int.init) ?? (query.get("days") == nil ? 30 : 0)
    let limit = query.get("limit").flatMap(Int.init) ?? (query.get("limit") == nil ? 50 : 0)
    guard (1...90).contains(days), (1...100).contains(limit) else { return try Self.failure(.badRequest, "Invalid Analytics Range") }
    let cursor = query.get("cursor") ?? ""
    if !cursor.isEmpty, (try? AnalyticsSubject(cursor).did) != author { return try Self.failure(.badRequest, "Invalid Analytics Cursor") }
    if path == "/v1/analytics/refresh" {
      let inventory: (items: [AnalyticsItem], complete: Bool)
      do { inventory = try await provider.inventory(author: author, at: at) }
      catch { return try Self.failure(.badGateway, "Analytics Provider Unavailable") }
      do { try await store.save(author: author, items: inventory.items, complete: inventory.complete, at: at) }
      catch { return try Self.failure(.serviceUnavailable, "Analytics Unavailable") }
    }
    return try Self.json(try await store.summary(author: author, days: days, limit: limit, cursor: cursor, at: at))
  }

  private static func json<T: Encodable>(_ value: T, status: HTTPResponse.Status = .ok) throws -> Response {
    Response(status: status, headers: [.contentType: "application/json"],
      body: .init(byteBuffer: .init(data: try AnalyticsCoding.encoder().encode(value))))
  }
  private static func failure(_ status: HTTPResponse.Status, _ message: String) throws -> Response {
    try json(["error": message], status: status)
  }
}
