import Crypto
import Foundation
import Hummingbird
import HummingbirdTesting
import HTTPTypes
import AsyncHTTPClient
import GatewayCore
import Logging
import NIOCore
import PostgresNIO
import Testing
import ThinAppViewCore
@testable import AppView

@Suite(.serialized, .enabled(if: ProcessInfo.processInfo.environment["SOCIAL_DRAFT_TEST_DATABASE_URL"] != nil))
struct SocialDraftStorageTests {
  private let png = Data([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])

  @Test func accountQuotaChargesOutstandingReservations() async throws {
    try await fixture { service, _, _ in
      let owner = self.owner()
      for _ in 0..<10 {
        let reservation = SocialDraftReserveInput(assetId: UUID().uuidString.lowercased(), name: "large.mp4", contentType: "video/mp4",
          byteLength: 100 << 20, sha256: String(repeating: "a", count: 64), kind: "video")
        _ = try await service.reserve(owner: owner, input: reservation)
      }
      let overflow = SocialDraftReserveInput(assetId: UUID().uuidString.lowercased(), name: "large.mp4", contentType: "video/mp4",
        byteLength: 100 << 20, sha256: String(repeating: "a", count: 64), kind: "video")
      await #expect(throws: SocialDraftStorageError.quota) { try await service.reserve(owner: owner, input: overflow) }
    }
  }

  @Test func sharedAttachmentSurvivesUntilFinalManifestDetaches() async throws {
    try await fixture { service, _, _ in
      let owner = self.owner()
      let input = self.reservation()
      _ = try await service.reserve(owner: owner, input: input)
      _ = try await service.upload(owner: owner, id: input.assetId, body: RequestBody(buffer: ByteBuffer(bytes: self.png)), contentLength: Int64(self.png.count))
      let refs = [SocialDraftManifestReference(assetId: input.assetId, role: "original", ref: "image:0")]
      _ = try await service.putManifest(owner: owner, input: .init(draftId: "3abcdefghijkl", expectedRevision: 0, attachments: refs))
      _ = try await service.putManifest(owner: owner, input: .init(draftId: "3bcdefghijklm", expectedRevision: 0, attachments: refs))
      _ = try await service.putManifest(owner: owner, input: .init(draftId: "3abcdefghijkl", expectedRevision: 1, attachments: []), deleted: true)
      do {
        let download = try await service.download(owner: owner, id: input.assetId)
        #expect(try Data(contentsOf: download.file.url) == self.png)
        withExtendedLifetime(download) {}
      }
      _ = try await service.putManifest(owner: owner, input: .init(draftId: "3bcdefghijklm", expectedRevision: 1, attachments: []), deleted: true)
      await #expect(throws: SocialDraftStorageError.notFound) { try await service.download(owner: owner, id: input.assetId) }
    }
  }

  @Test func actualRoutesValidateJSONAndStreamPrivateAttachments() async throws {
    try await fixture { service, _, _ in
      let owner = self.owner()
      let router = Router(context: GatewayRequestContext.self)
      router.add(middleware: SocialDraftTestAuth(owner: owner))
      SocialDraftRoutes(service: service).register(on: router.group())
      let app = Application(router: router, configuration: .init(address: .hostname("127.0.0.1", port: 0)))
      try await app.test(.live) { client in
        let input = self.reservation()
        var json = try #require(JSONSerialization.jsonObject(with: JSONEncoder().encode(input)) as? [String: Any])
        json["unknown"] = true
        let invalid = try await client.execute(uri: "/xrpc/app.thesocialwire.socialDraft.reserveAttachment", method: .post,
          headers: [.contentType: "application/json"], body: ByteBuffer(bytes: try JSONSerialization.data(withJSONObject: json)))
        #expect(invalid.status.code == 400)
        let reserved = try await client.execute(uri: "/xrpc/app.thesocialwire.socialDraft.reserveAttachment", method: .post,
          headers: [.contentType: "application/json"], body: ByteBuffer(bytes: try JSONEncoder().encode(input)))
        #expect(reserved.status.code == 200)
        #expect(reserved.headers[.cacheControl] == "private, no-store")
        let uploaded = try await client.execute(uri: "/xrpc/app.thesocialwire.socialDraft.uploadAttachment", method: .post,
          headers: [.contentType: "application/octet-stream", HTTPField.Name("X-Social-Draft-Upload-Id")!: input.assetId],
          body: ByteBuffer(bytes: self.png))
        #expect(uploaded.status.code == 200)
        let download = try await client.execute(uri: "/xrpc/app.thesocialwire.socialDraft.getAttachment?assetId=\(input.assetId)", method: .get)
        #expect(download.status.code == 200)
        #expect(download.headers[.cacheControl] == "private, no-store")
        #expect(Data(buffer: download.body) == self.png)
        #expect(try await client.execute(uri: "/xrpc/app.thesocialwire.socialDraft.getGifFile", method: .get).status.code == 404)
      }
    }
  }

  @Test func absoluteUploadDeadlineClosesStalledInputAndReleasesOwner() async throws {
    try await fixture { service, _, _ in
      let owner = self.owner()
      let input = self.reservation()
      _ = try await service.reserve(owner: owner, input: input)
      let router = Router(context: GatewayRequestContext.self)
      router.add(middleware: SocialDraftTestAuth(owner: owner))
      SocialDraftRoutes(service: service, transferSeconds: 0.1).register(on: router.group())
      let app = Application(router: router, configuration: .init(address: .hostname("127.0.0.1", port: 0)))
      try await app.test(.live) { client in
        let port = try #require(client.port)
        let http = HTTPClient(eventLoopGroupProvider: .singleton)
        let (stream, continuation) = AsyncThrowingStream<ByteBuffer, Error>.makeStream()
        continuation.yield(ByteBuffer(bytes: [0x89]))
        var request = HTTPClientRequest(url: "http://127.0.0.1:\(port)/xrpc/app.thesocialwire.socialDraft.uploadAttachment")
        request.method = .POST
        request.headers.add(name: "Content-Type", value: "application/octet-stream")
        request.headers.add(name: "X-Social-Draft-Upload-Id", value: input.assetId)
        request.body = .stream(stream, length: .known(Int64(self.png.count)))
        let started = ContinuousClock.now
        do {
          let response = try await http.execute(request, timeout: .seconds(3))
          _ = try await response.body.collect(upTo: 4096)
          Issue.record("Stalled upload unexpectedly succeeded")
        } catch { #expect(started.duration(to: .now) < .seconds(2)) }
        continuation.finish()
        try await http.shutdown()
        var released = false
        for _ in 0..<30 {
          do { _ = try await service.reserve(owner: owner, input: input); released = true; break }
          catch { try await Task.sleep(for: .milliseconds(10)) }
        }
        #expect(released)
      }
    }
  }

  @Test func reservationUploadManifestAndTenantIsolation() async throws {
    try await fixture { service, _, _ in
      let owner = self.owner()
      let input = self.reservation()
      let reserved = try await service.reserve(owner: owner, input: input)
      #expect(reserved.state == "reserved")
      #expect(try await service.reserve(owner: owner, input: input) == reserved)
      await #expect(throws: SocialDraftStorageError.uploadPending) { try await service.download(owner: owner, id: input.assetId) }
      let uploaded = try await service.upload(owner: owner, id: input.assetId,
        body: RequestBody(buffer: ByteBuffer(bytes: self.png)), contentLength: Int64(self.png.count))
      #expect(uploaded.state == "ready")
      do {
        let download = try await service.download(owner: owner, id: input.assetId)
        #expect(try Data(contentsOf: download.file.url) == self.png)
        await #expect(throws: SocialDraftStorageError.unavailable) { try await service.reserve(owner: owner, input: input) }
        withExtendedLifetime(download) {}
      }
      await #expect(throws: SocialDraftStorageError.notFound) { try await service.download(owner: self.owner(), id: input.assetId) }
      let attached = try await service.putManifest(owner: owner, input: .init(draftId: "3abcdefghijkl", expectedRevision: 0,
        attachments: [.init(assetId: input.assetId, role: "original", ref: "image:0")]))
      #expect(attached.revision == 1)
      #expect(attached.attachments.first?.asset == uploaded)
      await #expect(throws: SocialDraftStorageError.conflict) {
        try await service.putManifest(owner: owner, input: .init(draftId: "3abcdefghijkl", expectedRevision: 0, attachments: []))
      }
      let removed = try await service.putManifest(owner: owner,
        input: .init(draftId: "3abcdefghijkl", expectedRevision: 1, attachments: []), deleted: true)
      #expect(removed.deleted && removed.revision == 2 && removed.attachments.isEmpty)
      await #expect(throws: SocialDraftStorageError.notFound) { try await service.download(owner: owner, id: input.assetId) }
      await #expect(throws: SocialDraftStorageError.assetRetired) { try await service.reserve(owner: owner, input: input) }
    }
  }

  @Test func validatesContentAndKeepsMetadataEncrypted() async throws {
    try await fixture { service, pool, _ in
      let owner = self.owner()
      let input = self.reservation()
      _ = try await service.reserve(owner: owner, input: input)
      let rows = try await pool.query("SELECT metadata FROM private_social_draft_assets WHERE owner=\(owner) AND asset_id=\(input.assetId)::uuid", logger: Logger(label: "test"))
      for try await row in rows {
        let bytes = try row.decode(ByteBuffer.self)
        #expect(!Data(buffer: bytes).contains(Data(input.name.utf8)))
      }
      await #expect(throws: SocialDraftStorageError.invalid) {
        try await service.upload(owner: owner, id: input.assetId,
          body: RequestBody(buffer: ByteBuffer(string: "not a png")), contentLength: Int64(self.png.count))
      }
      let mismatch = SocialDraftReserveInput(assetId: input.assetId, name: "changed", contentType: input.contentType,
        byteLength: input.byteLength, sha256: input.sha256, kind: input.kind)
      await #expect(throws: SocialDraftStorageError.conflict) { try await service.reserve(owner: owner, input: mismatch) }
    }
  }

  @Test func durableCleanupRetriesAndReleasesQuotaOnlyAfterObjectDeletion() async throws {
    try await fixture { service, pool, objects in
      let owner = self.owner()
      let input = self.reservation()
      _ = try await service.reserve(owner: owner, input: input)
      _ = try await service.upload(owner: owner, id: input.assetId,
        body: RequestBody(buffer: ByteBuffer(bytes: self.png)), contentLength: Int64(self.png.count))
      _ = try await service.putManifest(owner: owner, input: .init(draftId: "3abcdefghijkl", expectedRevision: 0,
        attachments: [.init(assetId: input.assetId, role: "original", ref: "image:0")]))
      _ = try await service.putManifest(owner: owner, input: .init(draftId: "3abcdefghijkl", expectedRevision: 1, attachments: []))
      await objects.setDeleteFailure(true)
      await #expect(throws: SocialDraftStorageError.unavailable) { try await service.cleanup(limit: 100) }
      let pending = try await pool.query("""
        SELECT state,octet_length(metadata),attempts FROM private_social_draft_assets
        JOIN private_social_draft_cleanup USING(owner,asset_id) WHERE owner=\(owner) AND asset_id=\(input.assetId)::uuid
        """, logger: Logger(label: "test"))
      for try await row in pending {
        let (state, bytes, attempts) = try row.decode((String, Int, Int).self)
        #expect(state == "deleted" && bytes > 0 && attempts == 1)
      }
      await objects.setDeleteFailure(false)
      try await pool.query("UPDATE private_social_draft_cleanup SET available_at=now() WHERE owner=\(owner)", logger: Logger(label: "test"))
      #expect(try await service.cleanup(limit: 100) >= 1)
      let purged = try await pool.query("SELECT state,octet_length(metadata) FROM private_social_draft_assets WHERE owner=\(owner) AND asset_id=\(input.assetId)::uuid", logger: Logger(label: "test"))
      for try await row in purged {
        let (state, bytes) = try row.decode((String, Int).self)
        #expect(state == "purged" && bytes == 0)
      }
      await #expect(throws: SocialDraftStorageError.assetRetired) { try await service.reserve(owner: owner, input: input) }
    }
  }

  @Test func crossReplicaOwnerLockContentionFailsQuickly() async throws {
    try await fixture { service, pool, _ in
      let owner = self.owner()
      try await pool.query("INSERT INTO private_social_draft_accounts(owner) VALUES(\(owner))", logger: Logger(label: "test"))
      try await pool.withTransaction(logger: Logger(label: "test")) { connection in
        let lock = try await connection.query("SELECT owner FROM private_social_draft_accounts WHERE owner=\(owner) FOR UPDATE", logger: Logger(label: "test"))
        for try await _ in lock {}
        let started = ContinuousClock.now
        await #expect(throws: SocialDraftStorageError.unavailable) { try await service.reserve(owner: owner, input: self.reservation()) }
        #expect(started.duration(to: .now) < .seconds(2))
      }
    }
  }

  private func owner() -> String { "did:plc:draft-test-" + UUID().uuidString.lowercased() }
  private func reservation() -> SocialDraftReserveInput {
    .init(assetId: UUID().uuidString.lowercased(), name: "private-draft-name.png", contentType: "image/png",
      byteLength: Int64(png.count), sha256: SocialDraftService.hex(SHA256.hash(data: png)), kind: "photo")
  }

  private func fixture(_ body: @Sendable (SocialDraftService, PostgresClient, SocialDraftTestObjects) async throws -> Void) async throws {
    let url = try #require(ProcessInfo.processInfo.environment["SOCIAL_DRAFT_TEST_DATABASE_URL"])
    let logger = Logger(label: "social-draft.test")
    let pool = PostgresClient(configuration: try makePostgresConfig(from: url, logger: logger), backgroundLogger: logger)
    let run = Task { await pool.run() }
    let objects = SocialDraftTestObjects()
    let service = try SocialDraftService(pool: pool, storageKey: Data(0..<32), objects: objects, logger: logger)
    do { try await body(service, pool, objects) }
    catch { run.cancel(); await run.value; throw error }
    run.cancel(); await run.value
  }
}
