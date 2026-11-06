import AsyncHTTPClient
import Foundation
import GatewayCore
import HTTPTypes
import Hummingbird
import HummingbirdTesting
import Logging
import NIOCore
import Testing
@testable import Gateway

struct ArticleDraftProxyTests {
  @Test func privateArticleRoutesRequireIdentityAndNeverFallThroughToPDS() async throws {
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    do {
      let router = Router(context: GatewayRequestContext.self)
      SocialDraftProxyRoutes(baseURL: "http://upstream.invalid", internalSecret: nil, httpClient: http, admission: SocialDraftAdmission()).register(on: router.group())
      try await Application(router: router).test(.router) { client in
        for (name, method) in [("list", HTTPRequest.Method.get), ("get", .get), ("put", .post), ("delete", .post)] {
          let response = try await client.execute(uri: "/xrpc/app.thesocialwire.articleDraft.\(name)", method: method)
          #expect(response.status == .unauthorized)
          #expect(response.headers[.cacheControl] == "private, no-store")
        }
      }
    } catch { try? await http.shutdown(); throw error }
    try await http.shutdown()
  }

  @Test func articleListForwardsOnlyToTrustedAppViewWithOwnerAndPrivateHeaders() async throws {
    let path = "/xrpc/app.thesocialwire.articleDraft.list"
    let secret = String(repeating: "s", count: 32)
    let upstream = Router()
    upstream.get(RouterPath(path)) { request, _ -> Response in
      #expect(request.uri.queryParameters.get("limit") == "20")
      #expect(request.headers[.authorization] == "DPoP fixture")
      let timestamp = try #require(request.headers[HTTPField.Name(GatewayInternalTrust.timestampHeaderName)!])
      let signature = try #require(request.headers[HTTPField.Name(GatewayInternalTrust.signatureHeaderName)!])
      try GatewayInternalTrust.verify(secret: secret, did: "did:plc:viewer", method: "GET", pathWithQuery: path, timestamp: timestamp, signature: signature)
      return Response(status: .ok, headers: [.contentType: "application/json"], body: .init(byteBuffer: ByteBuffer(string: "{\"drafts\":[]}")))
    }
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    do {
      try await Application(router: upstream).test(.live) { upstreamClient in
        let port = try #require(upstreamClient.port)
        let router = Router(context: GatewayRequestContext.self)
        router.add(middleware: ArticleViewer())
        SocialDraftProxyRoutes(baseURL: "http://localhost:\(port)", internalSecret: secret, httpClient: http, admission: SocialDraftAdmission()).register(on: router.group())
        try await Application(router: router).test(.live) { client in
          let response = try await client.execute(uri: path + "?limit=20", method: .get)
          #expect(response.status == .ok)
          #expect(response.headers[.cacheControl] == "private, no-store")
          #expect(response.headers[HTTPField.Name("X-Content-Type-Options")!] == "nosniff")
        }
      }
    } catch { try? await http.shutdown(); throw error }
    try await http.shutdown()
  }

  private struct ArticleViewer: RouterMiddleware {
    func handle(_ request: Request, context: GatewayRequestContext, next: (Request, GatewayRequestContext) async throws -> Response) async throws -> Response {
      var context = context
      context.authContext = AuthContext(did: "did:plc:viewer", authorizationForwardingValue: "DPoP fixture", dpopProof: "fixture")
      return try await next(request, context)
    }
  }
}
