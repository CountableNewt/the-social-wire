import AsyncHTTPClient
import Foundation
import GatewayCore
import HTTPTypes
import Hummingbird
import HummingbirdTesting
import Logging
import Testing
import ThinAppViewCore

@testable import AppView

@Suite struct StandaloneIngressTests {
  @Test func internalTrustHeadersCannotAuthenticatePublicStandaloneListener() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer {
      for suffix in ["", "-wal", "-shm"] {
        try? FileManager.default.removeItem(atPath: path + suffix)
      }
    }
    let config = try AppViewServiceConfig.fromEnvironment([
      "APP_ENV": "local", "BACKEND_MODE": "standalone", "SQLITE_DB_PATH": path,
      "PDS_ATTESTATION_RECEIPT_SECRET": "standalone-test-attestation-secret-at-least-32-bytes",
      "GATEWAY_APPVIEW_INTERNAL_SECRET": "stale-secret-from-the-former-distributed-backend",
      "ENABLE_THIN_APPVIEW": "true", "TOOLS_ENABLED": "false",
      "OAUTH_IOS_METADATA_ORIGIN": "https://api.testing.thesocialwire.app",
    ])
    let logger = Logger(label: "standalone-ingress-test")
    let store = try SQLiteThinAppViewStore(path: path, logger: logger)
    let edge = try #require(config.standaloneEdge)
    let runtime = StandaloneEdgeRuntime(
      configuration: edge,
      cache: try SQLiteCache(path: path, logger: logger), environment: "local")
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    do {
      let router = AppViewRouterBuilder.router(
        config: config, httpClient: http,
        thinAppViewStore: store, projectionCache: nil, standaloneEdge: runtime, logger: logger)
      try await Application(router: router).test(.router) { client in
        let teapot = try await client.execute(uri: "/xrpc/app.thesocialwire.teapot", method: .get)
        #expect(teapot.status.code == 418)
        #expect(teapot.headers[.contentType] == "application/json")
        #expect(String(buffer: teapot.body) == #"{"error":"Teapot","message":"I'm a teapot"}"#)

        var headers: HTTPFields = [.authorization: "DPoP not-a-token"]
        for field in try GatewayInternalTrust.signedHeaders(
          secret: "stale-secret-from-the-former-distributed-backend",
          did: "did:plc:viewer", method: "POST", pathWithQuery: "/v1/media/project")
        {
          headers[HTTPField.Name(field.name)!] = field.value
        }
        let response = try await client.execute(
          uri: "/v1/media/project", method: .post,
          headers: headers, body: .init(string: #"{"mode":"photos","posts":[]}"#))
        #expect(response.status.code == 401)
        for edgePath in [
          "/v1/sync/preferences", "/v1/pds/cache/record", "/v1/subscription/entitlements",
          "/v1/media/gifs",
        ] {
          let unauthenticated = try await client.execute(uri: edgePath, method: .get)
          #expect(unauthenticated.status.code == 401)
        }
        let metadata = try await client.execute(uri: "/ios-client-metadata.json", method: .get)
        #expect(metadata.status.code == 200)
        let document =
          try JSONSerialization.jsonObject(with: Data(buffer: metadata.body)) as? [String: Any]
        #expect(
          document?["redirect_uris"] as? [String] == [
            "app.thesocialwire.testing.api:/oauth/callback"
          ])
      }
    } catch {
      try? await http.shutdown()
      throw error
    }
    try await http.shutdown()
  }
}
