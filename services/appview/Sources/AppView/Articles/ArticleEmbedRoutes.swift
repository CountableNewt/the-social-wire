import Foundation
import GatewayCore
import Hummingbird
import HTTPTypes

struct ArticleEmbedRoutes: Sendable {
  let resolver: ArticleEmbedResolver
  var timeoutSeconds: Double = 12

  func register(on group: RouterGroup<GatewayRequestContext>) {
    for path in ["/v1/publications/article-embed", "/xrpc/app.thesocialwire.publication.getArticleEmbed"] {
      group.get(RouterPath(path)) { request, context in try await handle(request, context: context) }
    }
  }

  func handle(_ request: Request, context: GatewayRequestContext) async throws -> Response {
    guard let did = context.authContext?.did, !did.isEmpty,
      did != GatewayInternalTrustAuthMiddleware.anonymousDiscoveryDID else {
      return try failure(.unauthorized, message: "Authentication is required")
    }
    let deadline = SocialDraftDeadline(seconds: timeoutSeconds, closeTransport: {})
    defer { deadline.complete() }
    let raw = request.uri.queryParameters.get("url") ?? ""
    let work = Task { try await resolver.resolve(raw) }
    deadline.bind { work.cancel() }
    do {
      let value = try await withTaskCancellationHandler { try await work.value } onCancel: { work.cancel() }
      return Response(status: .ok, headers: [.contentType: "application/json", .cacheControl: "private, max-age=300"],
        body: .init(byteBuffer: .init(bytes: try JSONEncoder().encode(value))))
    } catch ArticleEmbedError.invalidURL { return try failure(.badRequest) }
    catch ArticleEmbedError.notFound { return try failure(.notFound) }
    catch { return try failure(.badGateway) }
  }

  private func failure(_ status: HTTPResponse.Status, message: String = "Verified article unavailable") throws -> Response {
    Response(status: status, headers: [.contentType: "application/json", .cacheControl: "no-store"],
      body: .init(byteBuffer: .init(bytes: try JSONEncoder().encode(["error": status.reasonPhrase, "message": message]))))
  }
}
