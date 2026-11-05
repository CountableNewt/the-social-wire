import Foundation
import Testing
@testable import Gateway

@Test func publishedScopesMatchTheCurrentGatewayContractExactly() throws {
  let service = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
  let source = try String(contentsOf: service.appendingPathComponent("internal/gateway/scopes.go"), encoding: .utf8)
  for (name, actual) in [("webScope", GatewayOAuthScopes.web), ("iosScope", GatewayOAuthScopes.native)] {
    let pattern = try NSRegularExpression(pattern: "const \(name) = \"([^\"]+)\"")
    let match = try #require(pattern.firstMatch(in: source, range: NSRange(source.startIndex..., in: source)))
    let range = try #require(Range(match.range(at: 1), in: source))
    #expect(actual == String(source[range]))
  }
}

@Test func metadataUsesGatewayClientIDAndConfiguredWebCallback() throws {
  let routes = GatewayOAuthMetadataRoutes(publicOrigin: "https://testing.thesocialwire.app/", nativeOrigin: "https://api.testing.thesocialwire.app", operationsOrigin: nil, podcastsEnabled: false)
  let web = try routes.document(path: "/oauth-client-metadata.json", requestOrigin: "https://api.testing.thesocialwire.app")
  #expect(web.client_id == "https://api.testing.thesocialwire.app/oauth-client-metadata.json")
  #expect(web.redirect_uris == ["https://testing.thesocialwire.app/callback"])
  #expect(!web.scope.split(separator: " ").contains("blob:video/mp4"))
  #expect(web.scope.contains("repo:app.bsky.feed.post?action=create"))
  let native = try routes.document(path: "/ios-client-metadata.json", requestOrigin: "https://internal.invalid")
  #expect(native.client_id == "https://api.testing.thesocialwire.app/ios-client-metadata.json")
  #expect(native.redirect_uris == ["app.thesocialwire.testing.api:/oauth/callback"])
  #expect(native.scope.contains("repo:app.standard-reader.list?"))
  #expect(throws: (any Error).self) {
    try routes.document(path: "/operations-oauth-client-metadata.json", requestOrigin: "https://api.testing.thesocialwire.app")
  }
}
