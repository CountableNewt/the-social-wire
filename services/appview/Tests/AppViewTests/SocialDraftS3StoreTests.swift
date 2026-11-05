import AsyncHTTPClient
import Foundation
import Hummingbird
import HummingbirdTesting
import NIOConcurrencyHelpers
import Testing
@testable import AppView

@Suite struct SocialDraftS3StoreTests {
  @Test func signsAndStreamsPathStyleObjectRequests() async throws {
    let objects = NIOLockedValueBox([String: ByteBuffer]())
    let router = Router()
    router.put("/fixture-bucket/:key") { request, context async throws -> HTTPResponse.Status in
      #expect(request.headers[.authorization]?.hasPrefix("AWS4-HMAC-SHA256 ") == true)
      let key = try context.parameters.require("key")
      let bytes = try await request.body.collect(upTo: 4096)
      #expect(request.headers[.contentLength] == String(bytes.readableBytes))
      objects.withLockedValue { $0[key] = bytes }
      return .ok
    }
    router.get("/fixture-bucket/:key") { _, context async throws -> Response in
      let key = try context.parameters.require("key")
      let value = try #require(objects.withLockedValue { $0[key] })
      return Response(status: .ok, headers: [.contentType: "application/octet-stream"], body: .init(byteBuffer: value))
    }
    router.delete("/fixture-bucket/:key") { _, context async throws -> HTTPResponse.Status in
      let key = try context.parameters.require("key")
      _ = objects.withLockedValue { $0.removeValue(forKey: key) }
      return .noContent
    }
    let app = Application(router: router, configuration: .init(address: .hostname("127.0.0.1", port: 0)))
    try await app.test(.live) { client in
      let port = try #require(client.port)
      let http = HTTPClient(eventLoopGroupProvider: .singleton)
      // HummingbirdTesting binds localhost, which may select IPv6 on Linux.
      let store = SocialDraftS3Store(configuration: .init(endpoint: "http://localhost:\(port)",
        bucket: "fixture-bucket", accessKeyID: "fixture-access", secretAccessKey: "fixture-secret",
        region: "us-east-1", storageKey: Data(repeating: 1, count: 32)), http: http)
      do {
        let source = try SocialDraftTemporaryFile()
        let bytes = Data("encrypted-object-fixture".utf8)
        try bytes.write(to: source.url)
        try await store.put(key: "opaque-key", file: source, length: bytes.count)
        let result = try SocialDraftTemporaryFile()
        try await store.get(key: "opaque-key", into: result, maximumBytes: bytes.count)
        #expect(try Data(contentsOf: result.url) == bytes)
        try await store.delete(key: "opaque-key")
        #expect(objects.withLockedValue { $0.isEmpty })
      } catch { try? await store.shutdown(); try? await http.shutdown(); throw error }
      try await store.shutdown()
      try await http.shutdown()
    }
  }
}
