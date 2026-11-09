import Crypto
import Foundation
import GRDB
import Logging
import NIOCore
import NIOFoundationCompat

/// A single backend owns this store. Owner leases span object transfers, while SQLite
/// transactions stay short so one slow upload never holds the database writer lock.
final class SQLiteSocialDraftService: SocialDraftStoring {
  let database: DatabaseQueue
  let cipher: SocialDraftAttachmentCipher
  let objects: any SocialDraftObjectStoring
  let logger: Logger
  let admission = SocialDraftAdmission(limit: 8)

  init(path: String, storageKey: Data, objects: any SocialDraftObjectStoring, logger: Logger) throws {
    var configuration = Configuration()
    configuration.busyMode = .timeout(5)
    configuration.foreignKeysEnabled = true
    database = try DatabaseQueue(path: path, configuration: configuration)
    cipher = try SocialDraftAttachmentCipher(key: storageKey)
    self.objects = objects
    self.logger = logger
    try database.writeWithoutTransaction { db in try db.execute(sql: "PRAGMA journal_mode=WAL") }
    try database.write { db in try Self.migrate(db) }
  }

  func asset(owner: String, id: String, in db: Database) throws -> (SocialDraftAsset, String) {
    guard let row = try Row.fetchOne(db, sql: "SELECT metadata,object_key,state FROM private_social_draft_assets WHERE owner=? AND asset_id=?", arguments: [owner, id.lowercased()]) else { throw SocialDraftStorageError.notFound }
    let state: String = row["state"]
    guard state != "deleted", state != "purged" else { throw SocialDraftStorageError.notFound }
    do {
      let bytes: Data = row["metadata"]
      var value = try JSONDecoder().decode(SocialDraftAsset.self, from: cipher.openMetadata(bytes, owner: owner, assetID: id.lowercased()))
      value.state = state
      return (value, row["object_key"])
    } catch { throw SocialDraftStorageError.unavailable }
  }

  func reserve(owner: String, input: SocialDraftReserveInput) async throws -> SocialDraftAsset {
    guard SocialDraftValidation.reserve(input), SocialDraftValidation.owner(owner) else { throw SocialDraftStorageError.invalid }
    let lease = try admission.acquire(owner: owner)
    defer { withExtendedLifetime(lease) {} }
    let id = input.assetId.lowercased()
    return try await database.write { db in
      do {
        let (existing, _) = try asset(owner: owner, id: id, in: db)
        guard existing.name == input.name, existing.contentType == input.contentType, existing.byteLength == input.byteLength, existing.sha256 == input.sha256, existing.kind == input.kind else { throw SocialDraftStorageError.conflict }
        return existing
      } catch SocialDraftStorageError.notFound {}
      guard try !Bool.fetchOne(db, sql: "SELECT EXISTS(SELECT 1 FROM private_social_draft_assets WHERE owner=? AND asset_id=?)", arguments: [owner, id])! else { throw SocialDraftStorageError.assetRetired }
      let quota = try Row.fetchOne(db, sql: "SELECT coalesce(sum(byte_length),0) AS used,count(*) AS count FROM private_social_draft_assets WHERE owner=? AND state<>'purged'", arguments: [owner])!
      let used: Int64 = quota["used"], count: Int = quota["count"]
      guard used + input.byteLength <= SocialDraftValidation.maximumAccountBytes, count < 10000 else { throw SocialDraftStorageError.quota }
      let value = SocialDraftAsset(assetId: id, name: input.name, contentType: input.contentType, sha256: input.sha256, kind: input.kind, state: "reserved", byteLength: input.byteLength)
      let metadata = try cipher.sealMetadata(JSONEncoder().encode(value), owner: owner, assetID: id)
      let key = SymmetricKey(size: .bits256).withUnsafeBytes { SocialDraftService.hex(Data($0)) }
      try db.execute(sql: "INSERT INTO private_social_draft_accounts(owner) VALUES(?) ON CONFLICT DO NOTHING", arguments: [owner])
      try db.execute(sql: "INSERT INTO private_social_draft_assets(owner,asset_id,metadata,byte_length,object_key,state,created_at) VALUES(?,?,?,?,?,'reserved',?)", arguments: [owner, id, metadata, input.byteLength, key, Date().timeIntervalSince1970])
      try db.execute(sql: "INSERT INTO private_social_draft_cleanup(owner,asset_id,available_at,attempts) VALUES(?,?,?,0)", arguments: [owner, id, Date().addingTimeInterval(86400).timeIntervalSince1970])
      return value
    }
  }

  func upload<Body: AsyncSequence & Sendable>(owner: String, id rawID: String, body: Body, contentLength: Int64) async throws -> SocialDraftAsset where Body.Element == ByteBuffer {
    guard SocialDraftValidation.assetID(rawID), SocialDraftValidation.owner(owner), contentLength > 0, contentLength <= SocialDraftValidation.maximumFileBytes else { throw SocialDraftStorageError.invalid }
    let id = rawID.lowercased()
    let lease = try admission.acquire(owner: owner)
    defer { withExtendedLifetime(lease) {} }
    let (reserved, key) = try await database.read { try asset(owner: owner, id: id, in: $0) }
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
    guard received == contentLength, SocialDraftService.hex(hash.finalize()) == reserved.sha256 else { throw SocialDraftStorageError.invalid }
    try SocialDraftContentValidation.validate(file: plain, asset: reserved)
    if reserved.state == "ready" { return reserved }
    let encrypted = try SocialDraftTemporaryFile()
    let source = try FileHandle(forReadingFrom: plain.url)
    let target = try FileHandle(forWritingTo: encrypted.url)
    do {
      try cipher.encrypt(input: source, output: target, owner: owner, assetID: id, expectedBytes: Int(contentLength))
      try source.close(); try target.close()
    } catch { try? source.close(); try? target.close(); throw error }
    guard let length = try FileManager.default.attributesOfItem(atPath: encrypted.url.path)[.size] as? NSNumber else { throw SocialDraftStorageError.unavailable }
    try await objects.put(key: key, file: encrypted, length: length.intValue)
    try Task.checkCancellation()
    return try await database.write { db in
      var (current, storedKey) = try asset(owner: owner, id: id, in: db)
      guard current == reserved, storedKey == key else { throw SocialDraftStorageError.conflict }
      try db.execute(sql: "UPDATE private_social_draft_assets SET state='ready' WHERE owner=? AND asset_id=?", arguments: [owner, id])
      current.state = "ready"
      return current
    }
  }

  func download(owner: String, id rawID: String) async throws -> SocialDraftDownload {
    guard SocialDraftValidation.assetID(rawID), SocialDraftValidation.owner(owner) else { throw SocialDraftStorageError.invalid }
    let id = rawID.lowercased()
    let lease = try admission.acquire(owner: owner)
    let (value, key) = try await database.read { try asset(owner: owner, id: id, in: $0) }
    guard value.state == "ready" else { throw SocialDraftStorageError.uploadPending }
    let encrypted = try SocialDraftTemporaryFile()
    let frames = (Int(value.byteLength) + 65535) / 65536 + 1
    try await objects.get(key: key, into: encrypted, maximumBytes: Int(value.byteLength) + 6 + frames * 32)
    let plain = try SocialDraftTemporaryFile()
    let source = try FileHandle(forReadingFrom: encrypted.url)
    let target = try FileHandle(forWritingTo: plain.url)
    do {
      try cipher.decrypt(input: source, output: target, owner: owner, assetID: id, expectedBytes: Int(value.byteLength))
      try source.close(); try target.close()
    } catch { try? source.close(); try? target.close(); throw SocialDraftStorageError.unavailable }
    var hash = SHA256()
    for try await buffer in SocialDraftFileSequence(file: plain) { hash.update(data: Data(buffer: buffer)) }
    guard SocialDraftService.hex(hash.finalize()) == value.sha256 else { throw SocialDraftStorageError.unavailable }
    return SocialDraftDownload(asset: value, file: plain, lease: lease)
  }
}
