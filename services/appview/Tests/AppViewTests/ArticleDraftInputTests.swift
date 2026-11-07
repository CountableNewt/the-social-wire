import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import HTTPTypes
import NIOCore
import Testing
@testable import AppView

@Suite struct ArticleDraftInputTests {
  @Test func privateRoutesRejectUnauthenticatedAndUnavailableStorage() async throws {
    for owner in [nil, GatewayInternalTrustAuthMiddleware.anonymousDiscoveryDID, "did:plc:viewer"] {
      let router = Router(context: GatewayRequestContext.self)
      router.add(middleware: SocialDraftTestAuth(owner: owner))
      ArticleDraftRoutes(service: nil).register(on: router.group())
      try await Application(router: router).test(.router) { client in
        for (name, method) in [("list", HTTPRequest.Method.get), ("get", .get), ("put", .post), ("delete", .post)] {
          let response = try await client.execute(uri: "/xrpc/app.thesocialwire.articleDraft.\(name)", method: method)
          #expect(response.status.code == (owner == "did:plc:viewer" ? 503 : 401))
          #expect(response.headers[.cacheControl] == "private, no-store")
          #expect(response.headers[HTTPField.Name("X-Content-Type-Options")!] == "nosniff")
        }
      }
    }
  }

  @Test func payloadCipherBindsDraftAndOwnerWithoutPublishingAnything() throws {
    let cipher = try SocialDraftAttachmentCipher(key: Data(0..<32))
    let data = Data("private unpublished article".utf8)
    let encrypted = try cipher.sealMetadata(data, owner: "did:plc:a", assetID: "article-draft:one")
    #expect(encrypted != data)
    #expect(try cipher.openMetadata(encrypted, owner: "did:plc:a", assetID: "article-draft:one") == data)
    #expect(throws: (any Error).self) { try cipher.openMetadata(encrypted, owner: "did:plc:b", assetID: "article-draft:one") }
    #expect(throws: (any Error).self) { try cipher.openMetadata(encrypted, owner: "did:plc:a", assetID: "article-draft:two") }
  }

  @Test func rejectsInvalidAssetsAndPendingPublicationWithoutClaim() throws {
    let draft = try articleDraftFixture()
    #expect(ArticleDraftValidation.payload(draft))
    #expect(!ArticleDraftValidation.identifier(draft.id + "\n"))
    #expect(ArticleDraftValidation.references([], draft: draft))
    let invalid = try articleDraftFixture(overrides: ["publishingRecordKey": "3abcdefghijkl"])
    #expect(!ArticleDraftValidation.payload(invalid))
    let claimed = try articleDraftFixture(id: draft.id, overrides: ["publishingRecordKey": "3abcdefghijkl", "publicationClaimId": UUID().uuidString.lowercased()])
    #expect(ArticleDraftValidation.payload(claimed))
    #expect(draft.hasSameContent(as: claimed))
    #expect(!draft.hasSameContent(as: try articleDraftFixture(overrides: ["markdown": "Changed"])))
  }
}

func articleDraftFixture(id: String = UUID().uuidString.lowercased(), overrides: [String: Any] = [:]) throws -> ArticleDraftPayload {
  var value: [String: Any] = ["id": id, "title": "Private Title", "markdown": "Private Body", "excerpt": "Private Excerpt", "path": "/private", "tags": ["private"], "publicationUri": "", "createdAt": "2026-11-01T12:00:00Z", "updatedAt": "2026-11-01T12:00:00Z", "assets": []]
  for (key, item) in overrides { value[key] = item }
  return try JSONDecoder().decode(ArticleDraftPayload.self, from: JSONSerialization.data(withJSONObject: value))
}
