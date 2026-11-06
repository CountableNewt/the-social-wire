import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import Testing
import ThinAppViewCore
@testable import AppView

@Suite struct LegacyReadStateMutationMiddlewareTests {
  @Test func authorityTransitionReturnsConflictAndPreservesOtherErrors() async throws {
    let router = Router(context: GatewayRequestContext.self)
    router.add(middleware: LegacyReadStateMutationMiddleware())
    router.post("/v1/appview/read-marks") { _, _ async throws -> HTTPResponse.Status in
      throw PDSReadStateStorageError.alreadyMigrated
    }
    router.post("/invalid") { _, _ async throws -> HTTPResponse.Status in
      throw HTTPError(.badRequest)
    }
    let app = Application(router: router,
      configuration: .init(address: .hostname("127.0.0.1", port: 0)))
    try await app.test(.live) { client in
      let response = try await client.execute(uri: "/v1/appview/read-marks", method: .post)
      #expect(response.status.code == 409)
      let body = try #require(JSONSerialization.jsonObject(with: Data(buffer: response.body)) as? [String: Any])
      #expect(body["error"] as? String == "PDSReadStateRequired")
      #expect(body["retryable"] as? Bool == false)
      #expect(try await client.execute(uri: "/invalid", method: .post).status.code == 400)
    }
  }
}
