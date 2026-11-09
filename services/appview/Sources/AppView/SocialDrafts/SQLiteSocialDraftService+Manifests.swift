import Foundation
import GRDB

extension SQLiteSocialDraftService {
  func manifest(owner: String, id: String, in db: Database) throws -> SocialDraftManifest {
    guard let row = try Row.fetchOne(db, sql: "SELECT revision,deleted FROM private_social_draft_manifests WHERE owner=? AND draft_id=?", arguments: [owner, id]) else { throw SocialDraftStorageError.notFound }
    var value = SocialDraftManifest(draftId: id, revision: row["revision"], deleted: row["deleted"], attachments: [])
    guard !value.deleted else { return value }
    for binding in try Row.fetchAll(db, sql: "SELECT asset_id,role,ref FROM private_social_draft_bindings WHERE owner=? AND draft_id=? ORDER BY position", arguments: [owner, id]) {
      let assetID: String = binding["asset_id"]
      let (attachment, _) = try asset(owner: owner, id: assetID, in: db)
      value.attachments.append(.init(assetId: assetID, role: binding["role"], ref: binding["ref"], asset: attachment))
    }
    return value
  }

  func manifest(owner: String, id: String) async throws -> SocialDraftManifest {
    guard SocialDraftValidation.draftID(id), SocialDraftValidation.owner(owner) else { throw SocialDraftStorageError.invalid }
    let lease = try admission.acquire(owner: owner)
    defer { withExtendedLifetime(lease) {} }
    return try await database.read { try manifest(owner: owner, id: id, in: $0) }
  }

  func putManifest(owner: String, input: SocialDraftPutManifestInput, deleted: Bool = false) async throws -> SocialDraftManifest {
    guard SocialDraftValidation.owner(owner), SocialDraftValidation.draftID(input.draftId), input.expectedRevision >= 0, input.expectedRevision < Int64.max, SocialDraftValidation.references(input.attachments) else { throw SocialDraftStorageError.invalid }
    let lease = try admission.acquire(owner: owner)
    defer { withExtendedLifetime(lease) {} }
    return try await database.write { db in
      let previous: SocialDraftManifest
      do { previous = try manifest(owner: owner, id: input.draftId, in: db) }
      catch SocialDraftStorageError.notFound { previous = .init(draftId: input.draftId, revision: 0, deleted: false, attachments: []) }
      guard previous.revision == input.expectedRevision else { throw SocialDraftStorageError.conflict }
      for ref in input.attachments {
        let (attachment, _) = try asset(owner: owner, id: ref.assetId, in: db)
        guard attachment.state == "ready" else { throw SocialDraftStorageError.uploadPending }
      }
      try db.execute(sql: "INSERT INTO private_social_draft_accounts(owner) VALUES(?) ON CONFLICT DO NOTHING", arguments: [owner])
      try db.execute(sql: "INSERT INTO private_social_draft_manifests(owner,draft_id,revision,deleted) VALUES(?,?,?,?) ON CONFLICT(owner,draft_id) DO UPDATE SET revision=excluded.revision,deleted=excluded.deleted", arguments: [owner, input.draftId, input.expectedRevision + 1, deleted])
      try db.execute(sql: "DELETE FROM private_social_draft_bindings WHERE owner=? AND draft_id=?", arguments: [owner, input.draftId])
      for (position, ref) in input.attachments.enumerated() {
        let id = ref.assetId.lowercased()
        try db.execute(sql: "INSERT INTO private_social_draft_bindings(owner,draft_id,position,asset_id,role,ref) VALUES(?,?,?,?,?,?)", arguments: [owner, input.draftId, position, id, ref.role, ref.ref])
        try db.execute(sql: "DELETE FROM private_social_draft_cleanup WHERE owner=? AND asset_id=?", arguments: [owner, id])
      }
      for attachment in previous.attachments { try retireIfUnreferenced(owner: owner, id: attachment.assetId, in: db) }
      return try manifest(owner: owner, id: input.draftId, in: db)
    }
  }

  func isReferenced(owner: String, id: String, in db: Database) throws -> Bool {
    try Bool.fetchOne(db, sql: "SELECT EXISTS(SELECT 1 FROM private_social_draft_bindings WHERE owner=? AND asset_id=?) OR EXISTS(SELECT 1 FROM private_article_draft_bindings WHERE owner=? AND asset_id=?)", arguments: [owner, id, owner, id]) ?? false
  }

  func retireIfUnreferenced(owner: String, id: String, in db: Database) throws {
    guard try !isReferenced(owner: owner, id: id, in: db) else { return }
    try db.execute(sql: "UPDATE private_social_draft_assets SET state='deleted' WHERE owner=? AND asset_id=?", arguments: [owner, id])
    try db.execute(sql: "INSERT INTO private_social_draft_cleanup(owner,asset_id,available_at,attempts) VALUES(?,?,?,0) ON CONFLICT(owner,asset_id) DO UPDATE SET available_at=excluded.available_at,attempts=0", arguments: [owner, id, Date().timeIntervalSince1970])
  }
}
