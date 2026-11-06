import Crypto
import Foundation
import Hummingbird
import Logging
import NIOCore
import NIOFoundationCompat
import PostgresNIO

final class SocialDraftService: Sendable {
  let pool: PostgresClient
  let logger: Logger
  let cipher: SocialDraftAttachmentCipher
  let objects: any SocialDraftObjectStoring
  let admission: SocialDraftAdmission

  init(pool: PostgresClient, storageKey: Data, objects: any SocialDraftObjectStoring,
    maximumConnections: Int = 20, logger: Logger) throws {
    self.pool = pool
    self.logger = logger
    self.cipher = try SocialDraftAttachmentCipher(key: storageKey)
    self.objects = objects
    self.admission = SocialDraftAdmission(limit: max(1, min(8, maximumConnections / 4)))
  }

  func withOwner<Result: Sendable>(_ owner: String,
    body: @Sendable (PostgresConnection) async throws -> Result
  ) async throws -> Result {
    guard SocialDraftValidation.owner(owner) else { throw SocialDraftStorageError.invalid }
    try Task.checkCancellation()
    do {
      return try await pool.withTransaction(logger: logger) { connection in
        try await connection.query("SET LOCAL lock_timeout='100ms'", logger: logger)
        try await connection.query("""
          INSERT INTO private_social_draft_accounts(owner) VALUES(\(owner)) ON CONFLICT DO NOTHING
          """, logger: logger)
        let lock = try await connection.query("""
          SELECT owner FROM private_social_draft_accounts WHERE owner=\(owner) FOR UPDATE NOWAIT
          """, logger: logger)
        for try await _ in lock {}
        return try await body(connection)
      }
    } catch let transaction as PostgresTransactionError {
      if let error = transaction.closureError as? SocialDraftStorageError { throw error }
      throw SocialDraftStorageError.unavailable
    } catch {
      throw SocialDraftStorageError.unavailable
    }
  }

  func asset(owner: String, id: String, connection: PostgresConnection? = nil) async throws -> (SocialDraftAsset, String) {
    let query: PostgresQuery = """
      SELECT metadata,object_key,state FROM private_social_draft_assets
      WHERE owner=\(owner) AND asset_id=\(id)::uuid
      """
    let rows: PostgresRowSequence
    if let connection { rows = try await connection.query(query, logger: logger) }
    else { rows = try await pool.query(query, logger: logger) }
    for try await row in rows {
      let (metadata, key, state) = try row.decode((ByteBuffer, String, String).self)
      guard state != "deleted", state != "purged" else { throw SocialDraftStorageError.notFound }
      do {
        var asset = try JSONDecoder().decode(SocialDraftAsset.self,
          from: cipher.openMetadata(Data(buffer: metadata), owner: owner, assetID: id))
        asset.state = state
        return (asset, key)
      } catch { throw SocialDraftStorageError.unavailable }
    }
    throw SocialDraftStorageError.notFound
  }

  func reserve(owner: String, input: SocialDraftReserveInput) async throws -> SocialDraftAsset {
    guard SocialDraftValidation.reserve(input), SocialDraftValidation.owner(owner) else { throw SocialDraftStorageError.invalid }
    let lease = try admission.acquire(owner: owner)
    defer { withExtendedLifetime(lease) {} }
    return try await withOwner(owner) { connection in
      do {
        let (existing, _) = try await self.asset(owner: owner, id: input.assetId, connection: connection)
        guard existing.name == input.name, existing.contentType == input.contentType,
          existing.byteLength == input.byteLength, existing.sha256 == input.sha256, existing.kind == input.kind else {
          throw SocialDraftStorageError.conflict
        }
        return existing
      } catch SocialDraftStorageError.notFound { }
      let retired = try await connection.query("""
        SELECT EXISTS(SELECT 1 FROM private_social_draft_assets WHERE owner=\(owner) AND asset_id=\(input.assetId)::uuid)
        """, logger: self.logger)
      for try await row in retired {
        if try row.decode(Bool.self) { throw SocialDraftStorageError.assetRetired }
      }
      let quota = try await connection.query("""
        SELECT COALESCE(sum(byte_length),0)::bigint,count(*)
        FROM private_social_draft_assets WHERE owner=\(owner) AND state<>'purged'
        """, logger: self.logger)
      for try await row in quota {
        let (used, count) = try row.decode((Int64, Int64).self)
        guard used + input.byteLength <= SocialDraftValidation.maximumAccountBytes, count < 10000 else {
          throw SocialDraftStorageError.quota
        }
      }
      let asset = SocialDraftAsset(assetId: input.assetId, name: input.name, contentType: input.contentType,
        sha256: input.sha256, kind: input.kind, state: "reserved", byteLength: input.byteLength)
      let metadata = ByteBuffer(bytes: try self.cipher.sealMetadata(JSONEncoder().encode(asset), owner: owner, assetID: input.assetId))
      let key = SymmetricKey(size: .bits256).withUnsafeBytes { Data($0).map { String(format: "%02x", $0) }.joined() }
      try await connection.query("""
        INSERT INTO private_social_draft_assets(owner,asset_id,metadata,byte_length,object_key,state)
        VALUES(\(owner),\(input.assetId)::uuid,\(metadata),\(input.byteLength),\(key),'reserved')
        """, logger: self.logger)
      try await connection.query("""
        INSERT INTO private_social_draft_cleanup(owner,asset_id) VALUES(\(owner),\(input.assetId)::uuid)
        """, logger: self.logger)
      return asset
    }
  }

  func upload<Body: AsyncSequence & Sendable>(owner: String, id: String, body: Body, contentLength: Int64) async throws -> SocialDraftAsset
  where Body.Element == ByteBuffer {
    guard SocialDraftValidation.assetID(id), SocialDraftValidation.owner(owner),
      contentLength > 0, contentLength <= SocialDraftValidation.maximumFileBytes else { throw SocialDraftStorageError.invalid }
    let lease = try admission.acquire(owner: owner)
    defer { withExtendedLifetime(lease) {} }
    let (reserved, objectKey) = try await asset(owner: owner, id: id)
    guard reserved.byteLength == contentLength else { throw SocialDraftStorageError.invalid }
    let plain = try SocialDraftTemporaryFile()
    let input = try FileHandle(forWritingTo: plain.url)
    var hash = SHA256()
    var received: Int64 = 0
    do {
      for try await buffer in body {
        try Task.checkCancellation()
        received += Int64(buffer.readableBytes)
        guard received <= contentLength else { throw SocialDraftStorageError.invalid }
        let bytes = Data(buffer: buffer)
        hash.update(data: bytes)
        try input.write(contentsOf: bytes)
      }
      try input.close()
    } catch { try? input.close(); throw error }
    guard received == contentLength, Self.hex(hash.finalize()) == reserved.sha256 else { throw SocialDraftStorageError.invalid }
    try SocialDraftContentValidation.validate(file: plain, asset: reserved)
    let encrypted = try SocialDraftTemporaryFile()
    let source = try FileHandle(forReadingFrom: plain.url)
    let target = try FileHandle(forWritingTo: encrypted.url)
    do {
      try cipher.encrypt(input: source, output: target, owner: owner, assetID: id, expectedBytes: Int(contentLength))
      try source.close(); try target.close()
    } catch { try? source.close(); try? target.close(); throw error }
    let length = try FileManager.default.attributesOfItem(atPath: encrypted.url.path)[.size] as? NSNumber
    guard let length else { throw SocialDraftStorageError.unavailable }
    return try await withOwner(owner) { connection in
      let (stored, key) = try await self.asset(owner: owner, id: id, connection: connection)
      var current = stored
      var expected = reserved
      expected.state = current.state
      guard key == objectKey, current == expected else { throw SocialDraftStorageError.conflict }
      if current.state == "ready" { return current }
      // The durable reservation cleanup job survives ambiguous PUT/commit failures.
      try await self.objects.put(key: key, file: encrypted, length: length.intValue)
      try await connection.query("""
        UPDATE private_social_draft_assets SET state='ready' WHERE owner=\(owner) AND asset_id=\(id)::uuid
        """, logger: self.logger)
      current.state = "ready"
      return current
    }
  }

  func download(owner: String, id: String) async throws -> SocialDraftDownload {
    guard SocialDraftValidation.assetID(id), SocialDraftValidation.owner(owner) else { throw SocialDraftStorageError.invalid }
    let lease = try admission.acquire(owner: owner)
    let (asset, plain): (SocialDraftAsset, SocialDraftTemporaryFile) = try await withOwner(owner) { connection in
      let (asset, key) = try await self.asset(owner: owner, id: id, connection: connection)
      guard asset.state == "ready" else { throw SocialDraftStorageError.uploadPending }
      let encrypted = try SocialDraftTemporaryFile()
      let frames = (Int(asset.byteLength) + 65535) / 65536 + 1
      try await self.objects.get(key: key, into: encrypted, maximumBytes: Int(asset.byteLength) + 6 + frames * 32)
      let plain = try SocialDraftTemporaryFile()
      let source = try FileHandle(forReadingFrom: encrypted.url)
      let target = try FileHandle(forWritingTo: plain.url)
      do {
        try self.cipher.decrypt(input: source, output: target, owner: owner, assetID: id, expectedBytes: Int(asset.byteLength))
        try source.close(); try target.close()
      } catch { try? source.close(); try? target.close(); throw SocialDraftStorageError.unavailable }
      var hash = SHA256()
      for try await buffer in SocialDraftFileSequence(file: plain) { hash.update(data: Data(buffer: buffer)) }
      guard Self.hex(hash.finalize()) == asset.sha256 else { throw SocialDraftStorageError.unavailable }
      return (asset, plain)
    }
    return SocialDraftDownload(asset: asset, file: plain, lease: lease)
  }

  static func hex(_ bytes: some Sequence<UInt8>) -> String { bytes.map { String(format: "%02x", $0) }.joined() }
}
