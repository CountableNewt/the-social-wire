import AsyncHTTPClient
import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import Testing
@testable import Gateway

struct GIFReviewTests {
  @Test func rejectedPrivateGIFTransfersPreserveRetryAfterAndCannotBypassAdmissionWithBadSelectors() async throws {
    let admission = SocialDraftAdmission()
    let first = try await admission.acquire(owner: "did:plc:viewer", transfer: true)
    let second = try await admission.acquire(owner: "did:plc:viewer", transfer: true)
    do {
      try await withRoutes(admission: admission, apiKey: "fixture") { client in
        for query in ["id=one&rendition=gif", "id=../bad&rendition=gif"] {
          let response = try await client.execute(uri: "/xrpc/app.thesocialwire.socialDraft.getGifFile?\(query)", method: .get)
          #expect(response.status == .tooManyRequests)
          #expect(response.headers[.retryAfter] == "1")
          #expect(response.headers[.cacheControl] == "private, no-store")
        }
      }
    } catch { await admission.release(first); await admission.release(second); throw error }
    await admission.release(first)
    await admission.release(second)
  }

  @Test func configuredRedisOutageFailsClosedBeforePrivateGIFSelectorValidation() async throws {
    let admission = SocialDraftAdmission(redis: UnavailableDraftRedis())
    try await withRoutes(admission: admission, apiKey: "fixture") { client in
      let response = try await client.execute(uri: "/xrpc/app.thesocialwire.socialDraft.getGifFile?id=../bad&rendition=gif", method: .get)
      #expect(response.status == .serviceUnavailable)
      #expect(response.headers[.retryAfter] == "1")
    }
  }

  @Test func privateGIFValidationAndConfigurationFailuresReleaseTransferSlots() async throws {
    let admission = SocialDraftAdmission()
    try await withRoutes(admission: admission, apiKey: "") { client in
      for _ in 0..<4 {
        let invalid = try await client.execute(uri: "/xrpc/app.thesocialwire.socialDraft.getGifFile?id=../bad&rendition=gif", method: .get)
        #expect(invalid.status == .badRequest)
        let unavailable = try await client.execute(uri: "/xrpc/app.thesocialwire.socialDraft.getGifFile?id=one&rendition=gif", method: .get)
        #expect(unavailable.status == .serviceUnavailable)
      }
    }
    let first = try await admission.acquire(owner: "did:plc:viewer", transfer: true)
    let second = try await admission.acquire(owner: "did:plc:viewer", transfer: true)
    await admission.release(first)
    await admission.release(second)
  }

  @Test func webMDetectionMatchesGo126RuntimeExactSignature() {
    // Go1.26.6 net/http/sniff.go:163 uses an exact four-byte EBML signature.
    #expect(GIFMediaPolicy.detectedMIME(Data([0x1a, 0x45, 0xdf, 0xa3])) == "video/webm")
    #expect(GIFMediaPolicy.detectedMIME(Data([0x1a, 0x45, 0xdf])) == nil)
    #expect(GIFMediaPolicy.detectedMIME(Data([0x1a, 0x45, 0xdf, 0x00])) == nil)
  }

  private func withRoutes(admission: SocialDraftAdmission, apiKey: String, operation: @Sendable (any TestClientProtocol) async throws -> Void) async throws {
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    do {
      let router = Router(context: GatewayRequestContext.self)
      router.add(middleware: Viewer())
      GIFRoutes(catalog: GIFCatalogService(apiKey: apiKey, httpClient: http), limiter: WireRequestLimiter(), admission: admission).register(on: router.group())
      try await Application(router: router).test(.router, operation)
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
