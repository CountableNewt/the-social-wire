import Crypto
import Foundation
import GatewayCore
import GRDB
import Hummingbird
import HummingbirdTesting
import HTTPTypes
import Logging
import NIOCore
import Testing
@testable import AppView

@Suite struct SQLiteDraftStorageTests {
  private let png = Data([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])

  @Test func encryptedArticlePersistenceIsolationCASAndTombstones() async throws {
    try await fixture { service, path, objects in
      let owner = "did:plc:one", draft = try articleDraftFixture()
      let input = ArticleDraftPutInput(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: draft, attachments: [])
      let first = try await service.putArticleDraft(owner: owner, input: input)
      #expect(first.revision == 1 && first.draft == draft)
      #expect(try await service.putArticleDraft(owner: owner, input: input) == first)
      await #expect(throws: SocialDraftStorageError.notFound) { try await service.articleDraft(owner: "did:plc:other", id: draft.id) }
      let changed = ArticleDraftPutInput(expectedRevision: 0, mutationId: input.mutationId, draft: try articleDraftFixture(id: draft.id, overrides: ["title": "Changed"]), attachments: [])
      await #expect(throws: SocialDraftStorageError.conflict) { try await service.putArticleDraft(owner: owner, input: changed) }
      let encrypted = try await service.database.read { try Data.fetchOne($0, sql: "SELECT payload FROM private_article_drafts WHERE owner=?", arguments: [owner])! }
      #expect(encrypted.range(of: Data("Private Body".utf8)) == nil)
      let reopened = try SQLiteSocialDraftService(path: path, storageKey: Data(0..<32), objects: objects, logger: Logger(label: "sqlite-draft.test"))
      #expect(try await reopened.articleDraft(owner: owner, id: draft.id) == first)
      let deletion = ArticleDraftDeleteInput(draftId: draft.id, expectedRevision: 1, mutationId: UUID().uuidString.lowercased())
      let tombstone = try await service.deleteArticleDraft(owner: owner, input: deletion)
      #expect(tombstone.revision == 2 && tombstone.deleted && tombstone.draft == nil)
      #expect(try await service.deleteArticleDraft(owner: owner, input: deletion) == tombstone)
      let resurrection = ArticleDraftPutInput(expectedRevision: 2, mutationId: UUID().uuidString.lowercased(), draft: draft, attachments: [])
      await #expect(throws: SocialDraftStorageError.conflict) { try await service.putArticleDraft(owner: owner, input: resurrection) }
    }
  }

  @Test func sharedBindingsSurviveUntilFinalDetachAndCleanupRetriesPersist() async throws {
    try await fixture { service, path, objects in
      let owner = "did:plc:one", id = UUID().uuidString.lowercased(), localID = UUID().uuidString.lowercased()
      let input = reservation(id: id)
      _ = try await service.reserve(owner: owner, input: input)
      _ = try await service.upload(owner: owner, id: id, body: RequestBody(buffer: ByteBuffer(bytes: png)), contentLength: Int64(png.count))
      let refs = [SocialDraftManifestReference(assetId: id, role: "original", ref: "image:0")]
      _ = try await service.putManifest(owner: owner, input: .init(draftId: "3abcdefghijkl", expectedRevision: 0, attachments: refs))
      let draft = try articleDraftFixture(overrides: ["assets": [["id": localID, "alt": "Cover", "width": 10, "height": 10, "mimeType": "image/png", "name": "cover.png"]]])
      _ = try await service.putArticleDraft(owner: owner, input: .init(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: draft, attachments: [.init(localAssetId: localID, assetId: id)]))
      _ = try await service.putManifest(owner: owner, input: .init(draftId: "3abcdefghijkl", expectedRevision: 1, attachments: []), deleted: true)
      #expect(try await service.cleanup(limit: 100) == 0)
      do {
        let download = try await service.download(owner: owner, id: id)
        #expect(try Data(contentsOf: download.file.url) == png)
        withExtendedLifetime(download) {}
      }
      _ = try await service.deleteArticleDraft(owner: owner, input: .init(draftId: draft.id, expectedRevision: 1, mutationId: UUID().uuidString.lowercased()))
      await objects.setDeleteFailure(true)
      await #expect(throws: SocialDraftStorageError.unavailable) { try await service.cleanup(limit: 100) }
      let state = try await service.database.read { db -> (String, Int64) in
        let row = try Row.fetchOne(db, sql: "SELECT state,byte_length FROM private_social_draft_assets WHERE owner=? AND asset_id=?", arguments: [owner, id])!
        return (row["state"], row["byte_length"])
      }
      #expect(state.0 == "deleted")
      #expect(state.1 == Int64(png.count))
      let reopened = try SQLiteSocialDraftService(path: path, storageKey: Data(0..<32), objects: objects, logger: Logger(label: "sqlite-draft.test"))
      await objects.setDeleteFailure(false)
      try await reopened.database.write { try $0.execute(sql: "UPDATE private_social_draft_cleanup SET available_at=0") }
      #expect(try await reopened.cleanup(limit: 100) == 1)
      let purged = try await reopened.database.read { try String.fetchOne($0, sql: "SELECT state FROM private_social_draft_assets WHERE owner=? AND asset_id=?", arguments: [owner, id]) }
      #expect(purged == "purged")
      await #expect(throws: SocialDraftStorageError.assetRetired) { try await reopened.reserve(owner: owner, input: input) }
    }
  }

  @Test func outstandingReservationsAndArticleQuotaRemainEnforced() async throws {
    try await fixture { service, _, _ in
      let owner = "did:plc:quota"
      for _ in 0..<10 {
        _ = try await service.reserve(owner: owner, input: .init(assetId: UUID().uuidString.lowercased(), name: "large.mp4", contentType: "video/mp4", byteLength: 100 << 20, sha256: String(repeating: "a", count: 64), kind: "video"))
      }
      await #expect(throws: SocialDraftStorageError.quota) {
        try await service.reserve(owner: owner, input: .init(assetId: UUID().uuidString.lowercased(), name: "large.mp4", contentType: "video/mp4", byteLength: 100 << 20, sha256: String(repeating: "a", count: 64), kind: "video"))
      }
      let oversized = try articleDraftFixture(overrides: ["markdown": String(repeating: "x", count: 513 << 10)])
      await #expect(throws: SocialDraftStorageError.quota) { try await service.putArticleDraft(owner: owner, input: .init(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: oversized, attachments: [])) }
      for _ in 0..<200 { _ = try await service.putArticleDraft(owner: owner, input: .init(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: articleDraftFixture(), attachments: [])) }
      await #expect(throws: SocialDraftStorageError.quota) { try await service.putArticleDraft(owner: owner, input: .init(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: articleDraftFixture(), attachments: [])) }
      let page = try await service.articleDrafts(owner: owner, cursor: nil, limit: 20)
      #expect(page.drafts.count == 20 && page.cursor != nil)
      let first = try #require(page.drafts.first)
      let update = try articleDraftFixture(id: first.draftId, overrides: ["title": "Allowed Update"])
      #expect(try await service.putArticleDraft(owner: owner, input: .init(expectedRevision: 1, mutationId: UUID().uuidString.lowercased(), draft: update, attachments: [])).revision == 2)
    }
  }

  @Test func claimedContentCannotChangeAndConcurrentCreatesCannotOverwrite() async throws {
    try await fixture { service, _, _ in
      let owner = "did:plc:one", draftID = UUID().uuidString.lowercased()
      let claim = UUID().uuidString.lowercased()
      let draft = try articleDraftFixture(id: draftID, overrides: ["publishingRecordKey": "3abcdefghijkl", "publicationClaimId": claim])
      _ = try await service.putArticleDraft(owner: owner, input: .init(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: draft, attachments: []))
      let changed = try articleDraftFixture(id: draftID, overrides: ["title": "Changed", "publishingRecordKey": "3abcdefghijkl", "publicationClaimId": claim])
      await #expect(throws: SocialDraftStorageError.conflict) { try await service.putArticleDraft(owner: owner, input: .init(expectedRevision: 1, mutationId: UUID().uuidString.lowercased(), draft: changed, attachments: [])) }
      let next = try articleDraftFixture()
      let inputs = (0..<2).map { _ in ArticleDraftPutInput(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: next, attachments: []) }
      let successes = await withTaskGroup(of: Bool.self) { group in
        for input in inputs { group.addTask { do { _ = try await service.putArticleDraft(owner: owner, input: input); return true } catch { return false } } }
        var count = 0
        for await success in group { if success { count += 1 } }
        return count
      }
      #expect(successes == 1)
    }
  }

  @Test func routesKeepPrivateResponsesAndSupportSQLiteProtocolDispatch() async throws {
    try await fixture { service, _, _ in
      let router = Router(context: GatewayRequestContext.self)
      router.add(middleware: SocialDraftTestAuth(owner: "did:plc:one"))
      ArticleDraftRoutes(service: service).register(on: router.group())
      SocialDraftRoutes(service: service).register(on: router.group())
      try await Application(router: router).test(.router) { client in
        let draft = try articleDraftFixture()
        let input = ArticleDraftPutInput(expectedRevision: 0, mutationId: UUID().uuidString.lowercased(), draft: draft, attachments: [])
        let response = try await client.execute(uri: "/xrpc/app.thesocialwire.articleDraft.put", method: .post, headers: [.contentType: "application/json"], body: ByteBuffer(bytes: JSONEncoder().encode(input)))
        #expect(response.status.code == 200)
        #expect(response.headers[.cacheControl] == "private, no-store")
        let reserved = try await client.execute(uri: "/xrpc/app.thesocialwire.socialDraft.reserveAttachment", method: .post, headers: [.contentType: "application/json"], body: ByteBuffer(bytes: JSONEncoder().encode(reservation())))
        #expect(reserved.status.code == 200)
        #expect(reserved.headers[.cacheControl] == "private, no-store")
      }
    }
  }

  private func reservation(id: String = UUID().uuidString.lowercased()) -> SocialDraftReserveInput {
    .init(assetId: id, name: "cover.png", contentType: "image/png", byteLength: Int64(png.count), sha256: SocialDraftService.hex(SHA256.hash(data: png)), kind: "photo")
  }

  private func fixture(_ body: (SQLiteSocialDraftService, String, SocialDraftTestObjects) async throws -> Void) async throws {
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent("sqlite-drafts-" + UUID().uuidString)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let path = directory.appendingPathComponent("drafts.sqlite").path
    let objects = SocialDraftTestObjects()
    let service = try SQLiteSocialDraftService(path: path, storageKey: Data(0..<32), objects: objects, logger: Logger(label: "sqlite-draft.test"))
    try await body(service, path, objects)
  }
}
