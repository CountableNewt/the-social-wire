import Crypto
import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import HTTPTypes
import Logging
import NIOCore
import PostgresNIO
import Testing
import ThinAppViewCore
@testable import AppView

@Suite(.serialized, .enabled(if: ProcessInfo.processInfo.environment["SOCIAL_DRAFT_TEST_DATABASE_URL"] != nil))
struct ArticleDraftStorageTests {
  @Test func ownerIsolationCASRetryTombstonesAndEncryptedPersistence() async throws {
    try await fixture { service, pool, _ in
      let owner = self.owner(), other = self.owner(), draft = try articleDraftFixture()
      let mutation = UUID().uuidString.lowercased()
      let input = ArticleDraftPutInput(expectedRevision: 0, mutationId: mutation, draft: draft, attachments: [])
      let first = try await service.putArticleDraft(owner: owner, input: input)
      #expect(first.revision == 1)
      #expect(first.draft == draft)
      #expect(try await service.putArticleDraft(owner: owner, input: input) == first)
      await #expect(throws: SocialDraftStorageError.notFound) { try await service.articleDraft(owner: other, id: draft.id) }
      #expect(try await service.articleDrafts(owner: other, cursor: nil, limit: 20).drafts.isEmpty)
      let mismatch = ArticleDraftPutInput(expectedRevision: 0, mutationId: mutation, draft: try articleDraftFixture(id: draft.id, overrides: ["title": "Different"]), attachments: [])
      await #expect(throws: SocialDraftStorageError.conflict) { try await service.putArticleDraft(owner: owner, input: mismatch) }
      let stale = ArticleDraftPutInput(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: draft, attachments: [])
      await #expect(throws: SocialDraftStorageError.conflict) { try await service.putArticleDraft(owner: owner, input: stale) }
      let rows = try await pool.query("SELECT payload FROM private_article_drafts WHERE owner=\(owner) AND draft_id=\(draft.id)::uuid", logger: Logger(label: "test"))
      for try await row in rows {
        let encrypted = Data(buffer: try row.decode(ByteBuffer.self))
        #expect(encrypted.range(of: Data("Private Body".utf8)) == nil)
      }
      let deletion = ArticleDraftDeleteInput(draftId: draft.id, expectedRevision: 1, mutationId: UUID().uuidString.lowercased())
      let tombstone = try await service.deleteArticleDraft(owner: owner, input: deletion)
      #expect(tombstone.deleted && tombstone.draft == nil && tombstone.attachments.isEmpty)
      #expect(tombstone.revision == 2)
      #expect(try await service.deleteArticleDraft(owner: owner, input: deletion) == tombstone)
      #expect(try await service.articleDrafts(owner: owner, cursor: nil, limit: 20).drafts == [tombstone])
      let resurrect = ArticleDraftPutInput(expectedRevision: 2, mutationId: UUID().uuidString.lowercased(), draft: draft, attachments: [])
      await #expect(throws: SocialDraftStorageError.conflict) { try await service.putArticleDraft(owner: owner, input: resurrect) }
    }
  }

  @Test func paginationQuotasAndClaimedContentProtection() async throws {
    try await fixture { service, _, _ in
      let owner = self.owner()
      for _ in 0..<3 { _ = try await service.putArticleDraft(owner: owner, input: .init(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: articleDraftFixture(), attachments: [])) }
      let page = try await service.articleDrafts(owner: owner, cursor: nil, limit: 2)
      #expect(page.drafts.count == 2 && page.cursor != nil)
      let next = try await service.articleDrafts(owner: owner, cursor: page.cursor, limit: 2)
      #expect(next.drafts.count == 1 && next.cursor == nil)
      #expect(Set((page.drafts + next.drafts).map(\.draftId)).count == 3)
      let id = UUID().uuidString.lowercased(), claim = UUID().uuidString.lowercased()
      let draft = try articleDraftFixture(id: id, overrides: ["publishingRecordKey": "3abcdefghijkl", "publicationClaimId": claim])
      _ = try await service.putArticleDraft(owner: owner, input: .init(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: draft, attachments: []))
      let changed = try articleDraftFixture(id: id, overrides: ["title": "Changed", "publishingRecordKey": "3abcdefghijkl", "publicationClaimId": claim])
      await #expect(throws: SocialDraftStorageError.conflict) { try await service.putArticleDraft(owner: owner, input: .init(expectedRevision: 1, mutationId: UUID().uuidString.lowercased(), draft: changed, attachments: [])) }
      let rival = try articleDraftFixture(id: id, overrides: ["publishingRecordKey": "3abcdefghijkl", "publicationClaimId": UUID().uuidString.lowercased()])
      await #expect(throws: SocialDraftStorageError.conflict) { try await service.putArticleDraft(owner: owner, input: .init(expectedRevision: 1, mutationId: UUID().uuidString.lowercased(), draft: rival, attachments: [])) }
      let oversized = try articleDraftFixture(overrides: ["markdown": String(repeating: "x", count: 513 << 10)])
      await #expect(throws: SocialDraftStorageError.quota) { try await service.putArticleDraft(owner: owner, input: .init(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: oversized, attachments: [])) }
    }
  }

  @Test func articleBindingsProtectSharedAssetsThroughSocialDetachAndCleanup() async throws {
    try await fixture { service, pool, _ in
      let owner = self.owner(), id = UUID().uuidString.lowercased(), assetID = UUID().uuidString.lowercased(), localID = UUID().uuidString.lowercased()
      let png = Data([0x89,0x50,0x4e,0x47,0x0d,0x0a,0x1a,0x0a])
      _ = try await service.reserve(owner: owner, input: .init(assetId: assetID, name: "cover.png", contentType: "image/png", byteLength: Int64(png.count), sha256: SocialDraftService.hex(SHA256.hash(data: png)), kind: "photo"))
      _ = try await service.upload(owner: owner, id: assetID, body: RequestBody(buffer: ByteBuffer(bytes: png)), contentLength: Int64(png.count))
      let draft = try articleDraftFixture(id: id, overrides: ["assets": [["id": localID, "alt": "Cover", "width": 10, "height": 10, "mimeType": "image/png", "name": "cover.png"]], "coverAssetId": localID])
      let other = self.owner()
      await #expect(throws: SocialDraftStorageError.notFound) {
        try await service.putArticleDraft(owner: other, input: .init(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: draft, attachments: [.init(localAssetId: localID, assetId: assetID)]))
      }
      await #expect(throws: SocialDraftStorageError.notFound) { try await service.articleDraft(owner: other, id: id) }
      _ = try await service.putArticleDraft(owner: owner, input: .init(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: draft, attachments: [.init(localAssetId: localID, assetId: assetID)]))
      _ = try await service.putManifest(owner: owner, input: .init(draftId: "3abcdefghijkl", expectedRevision: 0, attachments: [.init(assetId: assetID, role: "original", ref: "cover")]))
      _ = try await service.putManifest(owner: owner, input: .init(draftId: "3abcdefghijkl", expectedRevision: 1, attachments: []), deleted: true)
      _ = try await service.cleanup(limit: 100)
      do {
        let download = try await service.download(owner: owner, id: assetID)
        #expect(try Data(contentsOf: download.file.url) == png)
        withExtendedLifetime(download) {}
      }
      _ = try await service.deleteArticleDraft(owner: owner, input: .init(draftId: id, expectedRevision: 1, mutationId: UUID().uuidString.lowercased()))
      await #expect(throws: SocialDraftStorageError.notFound) { try await service.download(owner: owner, id: assetID) }
      _ = try await service.cleanup(limit: 100)
      let states = try await pool.query("SELECT state FROM private_social_draft_assets WHERE owner=\(owner) AND asset_id=\(assetID)::uuid", logger: Logger(label: "test"))
      for try await row in states { #expect(try row.decode(String.self) == "purged") }
    }
  }

  @Test func realRoutesValidateInputCASAndOfflineDeleteTombstones() async throws {
    try await fixture { service, _, _ in
      let owner = self.owner(), draft = try articleDraftFixture()
      let router = Router(context: GatewayRequestContext.self)
      router.add(middleware: SocialDraftTestAuth(owner: owner))
      ArticleDraftRoutes(service: service).register(on: router.group())
      try await Application(router: router).test(.router) { client in
        let path = "/xrpc/app.thesocialwire.articleDraft."
        let input = ArticleDraftPutInput(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: draft, attachments: [])
        let saved = try await client.execute(uri: path + "put", method: .post, headers: [.contentType: "application/json"], body: ByteBuffer(bytes: JSONEncoder().encode(input)))
        #expect(saved.status.code == 200)
        #expect(saved.headers[.cacheControl] == "private, no-store")
        let stale = ArticleDraftPutInput(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: draft, attachments: [])
        #expect(try await client.execute(uri: path + "put", method: .post, headers: [.contentType: "application/json"], body: ByteBuffer(bytes: JSONEncoder().encode(stale))).status.code == 409)
        #expect(try await client.execute(uri: path + "get?draftId=invalid", method: .get).status.code == 400)
        #expect(try await client.execute(uri: path + "list?limit=21", method: .get).status.code == 400)
        #expect(try await client.execute(uri: path + "put", method: .post, headers: [.contentType: "application/json"], body: ByteBuffer(string: "{\"owner\":\"did:plc:other\"}")).status.code == 400)
        let offline = ArticleDraftDeleteInput(draftId: UUID().uuidString.lowercased(), expectedRevision: 0, mutationId: UUID().uuidString.lowercased())
        let deleted = try await client.execute(uri: path + "delete", method: .post, headers: [.contentType: "application/json"], body: ByteBuffer(bytes: JSONEncoder().encode(offline)))
        #expect(deleted.status.code == 200)
        let tombstone = try JSONDecoder().decode(ArticleDraftDocument.self, from: Data(buffer: deleted.body))
        #expect(tombstone.deleted && tombstone.revision == 1 && tombstone.draft == nil)
        let listed = try await client.execute(uri: path + "list", method: .get)
        let page = try JSONDecoder().decode(ArticleDraftPage.self, from: Data(buffer: listed.body))
        #expect(page.drafts.count == 2)
      }
    }
  }

  @Test func concurrentCreatesCannotOverwriteAndLiveDraftQuotaStillAllowsUpdates() async throws {
    try await fixture { service, _, _ in
      let owner = self.owner(), id = UUID().uuidString.lowercased()
      let inputs = try ["First Device", "Second Device"].map { title in
        ArticleDraftPutInput(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: try articleDraftFixture(id: id, overrides: ["title": title]), attachments: [])
      }
      let successes = await withTaskGroup(of: Bool.self) { group in
        for input in inputs {
          group.addTask {
            do { _ = try await service.putArticleDraft(owner: owner, input: input); return true }
            catch { return false }
          }
        }
        var count = 0
        for await saved in group { if saved { count += 1 } }
        return count
      }
      #expect(successes == 1)
      let first = try await service.articleDraft(owner: owner, id: id)
      #expect(first.revision == 1)
      for _ in 0..<199 {
        _ = try await service.putArticleDraft(owner: owner, input: .init(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: articleDraftFixture(), attachments: []))
      }
      await #expect(throws: SocialDraftStorageError.quota) {
        try await service.putArticleDraft(owner: owner, input: .init(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: articleDraftFixture(), attachments: []))
      }
      let updated = try await service.putArticleDraft(owner: owner, input: .init(expectedRevision: 1, mutationId: UUID().uuidString.lowercased(), draft: articleDraftFixture(id: id, overrides: ["title": "Allowed Update"]), attachments: []))
      #expect(updated.revision == 2)
    }
  }

  private func owner() -> String { "did:plc:article-test-" + UUID().uuidString.lowercased() }
  private func fixture(_ body: @Sendable (SocialDraftService, PostgresClient, SocialDraftTestObjects) async throws -> Void) async throws {
    let url = try #require(ProcessInfo.processInfo.environment["SOCIAL_DRAFT_TEST_DATABASE_URL"])
    let logger = Logger(label: "article-draft.test")
    let pool = PostgresClient(configuration: try makePostgresConfig(from: url, logger: logger), backgroundLogger: logger)
    let run = Task { await pool.run() }
    let objects = SocialDraftTestObjects()
    let service = try SocialDraftService(pool: pool, storageKey: Data(0..<32), objects: objects, logger: logger)
    do { try await body(service, pool, objects) }
    catch { run.cancel(); await run.value; throw error }
    run.cancel(); await run.value
  }
}
