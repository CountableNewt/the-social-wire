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

struct SocialDraftProxyTests {
  @Test func allPrivateRoutesRejectMissingIdentityWithoutContactingUpstream() async throws {
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    do {
      let router = Router(context: GatewayRequestContext.self)
      SocialDraftProxyRoutes(baseURL: "http://upstream.invalid", internalSecret: nil, httpClient: http, admission: SocialDraftAdmission()).register(on: router.group())
      try await Application(router: router).test(.router) { client in
        for (name, method) in [("getAttachment", HTTPRequest.Method.get), ("getManifest", .get), ("reserveAttachment", .post), ("uploadAttachment", .post), ("putManifest", .post), ("deleteManifest", .post)] {
          let response = try await client.execute(uri: "/xrpc/app.thesocialwire.socialDraft.\(name)", method: method)
          #expect(response.status == .unauthorized)
          #expect(response.headers[.cacheControl] == "private, no-store")
        }
      }
    } catch { try? await http.shutdown(); throw error }
    try await http.shutdown()
  }

  @Test func largeBinaryUploadStreamsWithOriginalOwnerTrustAndRateLimitStatus() async throws {
    let path = "/xrpc/app.thesocialwire.socialDraft.uploadAttachment"
    let secret = String(repeating: "s", count: 32)
    let bytes = ByteBuffer(repeating: 65, count: 5 << 20)
    let upstream = Router()
    upstream.post(RouterPath(path)) { request, _ -> Response in
      #expect(request.headers[.contentType] == "application/octet-stream")
      #expect(request.headers[HTTPField.Name("X-Social-Draft-Upload-Id")!] == "asset-id")
      let timestamp = try #require(request.headers[HTTPField.Name(GatewayInternalTrust.timestampHeaderName)!])
      let signature = try #require(request.headers[HTTPField.Name(GatewayInternalTrust.signatureHeaderName)!])
      try GatewayInternalTrust.verify(secret: secret, did: "did:plc:viewer", method: "POST", pathWithQuery: path, timestamp: timestamp, signature: signature)
      let received = try await request.body.collect(upTo: 6 << 20)
      #expect(received.readableBytes == bytes.readableBytes)
      return Response(status: .tooManyRequests, headers: [.retryAfter: "2"], body: .init(byteBuffer: ByteBuffer(string: "{}")))
    }
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    do {
      try await Application(router: upstream).test(.live) { upstreamClient in
        let port = try #require(upstreamClient.port)
        let router = Router(context: GatewayRequestContext.self)
        router.add(middleware: Viewer())
        SocialDraftProxyRoutes(baseURL: "http://localhost:\(port)", internalSecret: secret, httpClient: http, admission: SocialDraftAdmission()).register(on: router.group())
        try await Application(router: router).test(.live) { client in
          let response = try await client.execute(uri: path, method: .post, headers: [.contentType: "application/octet-stream", HTTPField.Name("X-Social-Draft-Upload-Id")!: "asset-id"], body: bytes)
          #expect(response.status == .tooManyRequests)
          #expect(response.headers[.retryAfter] == "2")
          #expect(response.headers[.cacheControl] == "private, no-store")
        }
      }
    } catch { try? await http.shutdown(); throw error }
    try await http.shutdown()
  }

  private struct Viewer: RouterMiddleware {
    func handle(_ request: Request, context: GatewayRequestContext, next: (Request, GatewayRequestContext) async throws -> Response) async throws -> Response {
      var context = context
      context.authContext = AuthContext(did: "did:plc:viewer", authorizationForwardingValue: "DPoP fixture", dpopProof: "fixture")
      return try await next(request, context)
    }
  }
}
