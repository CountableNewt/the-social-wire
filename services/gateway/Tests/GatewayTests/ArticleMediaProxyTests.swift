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

@Suite struct ArticleMediaProxyTests {
  @Test(arguments: ["/v1/publications/article-embed", "/xrpc/app.thesocialwire.publication.getArticleEmbed", "/v1/media/project"])
  func forwardsCanonicalPathViewerTrustAndCachePolicy(path: String) async throws {
    let media = path == "/v1/media/project"
    let target = media ? path : "/xrpc/app.thesocialwire.publication.getArticleEmbed"
    let method = media ? "POST" : "GET"
    let secret = String(repeating: "s", count: 32)
    let upstream = Router()
    let check: @Sendable (Request, BasicRequestContext) async throws -> Response = { request, _ in
      #expect(request.uri.path == target)
      let timestamp = try #require(request.headers[HTTPField.Name(GatewayInternalTrust.timestampHeaderName)!])
      let signature = try #require(request.headers[HTTPField.Name(GatewayInternalTrust.signatureHeaderName)!])
      try GatewayInternalTrust.verify(secret: secret, did: "did:plc:viewer", method: method,
        pathWithQuery: target, timestamp: timestamp, signature: signature)
      if media { #expect(String(buffer: try await request.body.collect(upTo: 1024)) == #"{"mode":"photos","posts":[]}"#) }
      else { #expect(request.uri.queryParameters.get("url") == "https://publisher.example/a?one=two&three=four") }
      return Response(status: .ok, headers: [.cacheControl: media ? "private, no-store" : "private, max-age=300"], body: .init(byteBuffer: .init(string: "{}")))
    }
    if media { upstream.post(RouterPath(target), use: check) } else { upstream.get(RouterPath(target), use: check) }
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    do {
      try await Application(router: upstream).test(.live) { upstreamClient in
        let port = try #require(upstreamClient.port)
        let router = Router(context: GatewayRequestContext.self)
        router.add(middleware: Viewer())
        AppViewProxyRoutes(baseURL: "http://localhost:\(port)", internalSecret: secret, httpClient: http,
          logger: Logger(label: "article-media-proxy-test")).register(on: router.group())
        try await Application(router: router).test(.live) { downstream in
          let uri = path + (media ? "" : "?url=https%3A%2F%2Fpublisher.example%2Fa%3Fone%3Dtwo%26three%3Dfour")
          let response = try await downstream.execute(uri: uri, method: media ? .post : .get,
            body: media ? ByteBuffer(string: #"{"mode":"photos","posts":[]}"#) : nil)
          #expect(response.status == .ok)
          #expect(response.headers[.cacheControl] == (media ? "private, no-store" : "private, max-age=300"))
        }
      }
    } catch { try? await http.shutdown(); throw error }
    try await http.shutdown()
  }

  private struct Viewer: RouterMiddleware {
    func handle(_ request: Request, context: GatewayRequestContext,
      next: (Request, GatewayRequestContext) async throws -> Response) async throws -> Response {
      var context = context
      context.authContext = AuthContext(did: "did:plc:viewer", authorizationForwardingValue: "DPoP fixture", dpopProof: "fixture")
      return try await next(request, context)
    }
  }
}
