import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import ThinAppViewCore
import Testing
@testable import AppView

@Suite("Private podcast storage failures")
struct PodcastPrivateStorageMiddlewareTests {
  @Test("storage failure returns safe 503 while public routes remain healthy")
  func unavailableStorage() async throws {
    let router = Router(context: GatewayRequestContext.self)
    let privateRoutes = router.group().add(middleware: PodcastPrivateStorageMiddleware())
    privateRoutes.post("/xrpc/app.thesocialwire.podcast.resolvePrivateShow") { _, _ -> Response in
      throw PodcastStoreError.privateStorageUnavailable
    }
    privateRoutes.get("/xrpc/app.thesocialwire.podcast.getShows") { _, _ -> Response in
      Response(status: .ok)
    }
    try await Application(router: router).test(.router) { client in
      let unavailable = try await client.execute(uri: "/xrpc/app.thesocialwire.podcast.resolvePrivateShow", method: .post)
      #expect(unavailable.status == .serviceUnavailable)
      let body = String(decoding: unavailable.body.readableBytesView, as: UTF8.self)
      #expect(body.contains("Private Podcast Storage Is Unavailable"))
      #expect(!body.contains("key") && !body.contains("token") && !body.contains("AES"))
      let publicShows = try await client.execute(uri: "/xrpc/app.thesocialwire.podcast.getShows", method: .get)
      #expect(publicShows.status == .ok)
    }
  }
}
