import AsyncHTTPClient
import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import Testing
@testable import Gateway

struct GIFMigrationTests {
  @Test func providerAssetsUseOnlyTrustedRenditionsAndNeverExposeProviderPayload() throws {
    let body = #"{"results":[{"id":"one","title":"Title","media_formats":{"gif":{"url":"https://static.klipy.com/ii/a/giphy.gif","dims":[640,360]},"preview":{"url":"https://static.klipy.com/ii/a/poster.jpg"},"mp4":{"url":"https://static.klipy.com/ii/a/video.mp4"}},"secret":"not-a-client-field"},{"id":"missing-formats"}],"next":"cursor"}"#
    let page = try JSONDecoder().decode(GIFProviderPage.self, from: Data(body.utf8))
    let assets = (page.results ?? []).compactMap(GIFMediaPolicy.normalize)
    #expect(assets.count == 1)
    #expect(assets[0].width == 640)
    #expect(assets[0].attribution == "Powered by KLIPY")
    #expect(!String(decoding: try JSONEncoder().encode(assets), as: UTF8.self).contains("secret"))
    for raw in ["http://static.klipy.com/ii/a.gif", "https://static.klipy.com.evil/ii/a.gif", "https://user@static.klipy.com/ii/a.gif", "https://static.klipy.com:443/ii/a.gif", "https://static.klipy.com/other/a.gif", "https://static.klipy.com/ii/a.gif#fragment"] {
      #expect(GIFMediaPolicy.mediaURL(raw) == nil)
    }
  }

  @Test func privateFilesRejectEncodedTraversalQueriesAndUnavailableFormats() throws {
    for raw in ["https://static.klipy.com/ii/a/../b.gif", "https://static.klipy.com/ii/%2e%2e/b.gif", "https://static.klipy.com/ii/a.gif?x=1", "https://static.klipy.com/ii/a.gif?", "https://static.klipy.com/ii/"] {
      #expect(GIFMediaPolicy.fileURL(raw) == nil)
    }
    #expect(GIFMediaPolicy.fileURL("https://static.klipy.com/ii/a/b.gif") != nil)
    #expect(!GIFMediaPolicy.allowedMIME("video/mp4", rendition: "gif"))
    #expect(!GIFMediaPolicy.allowedMIME("text/html", rendition: "poster"))
    #expect(GIFMediaPolicy.detectedMIME(Data("GIF89a".utf8)) == "image/gif")
    #expect(GIFMediaPolicy.detectedMIME(Data([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])) == "image/png")
    #expect(GIFMediaPolicy.detectedMIME(Data("<html>not an image</html>".utf8)) == nil)
    #expect(!GIFMediaPolicy.validID("../bad"))
  }

  @Test func fileSelectorsCannotSupplyURLsOrDuplicateIDs() async throws {
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    do {
      let router = Router(context: GatewayRequestContext.self)
      router.add(middleware: Viewer())
      GIFRoutes(catalog: GIFCatalogService(apiKey: "fixture", httpClient: http), limiter: WireRequestLimiter(), admission: SocialDraftAdmission()).register(on: router.group())
      try await Application(router: router).test(.router) { client in
        for query in ["id=one&rendition=gif&url=http://127.0.0.1", "id=one&id=two&rendition=gif", "id=one&rendition=unavailable", "id=../bad&rendition=gif"] {
          let response = try await client.execute(uri: "/xrpc/app.thesocialwire.socialDraft.getGifFile?\(query)", method: .get)
          #expect(response.status == .badRequest)
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
