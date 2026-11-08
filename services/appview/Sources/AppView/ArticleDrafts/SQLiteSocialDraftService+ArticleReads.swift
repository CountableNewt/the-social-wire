import Foundation
import GRDB

extension SQLiteSocialDraftService {
  func articleDraft(owner: String, id: String, in db: Database) throws -> ArticleDraftDocument {
    guard let row = try Row.fetchOne(db, sql: "SELECT revision,deleted,mutation_id,updated_at,payload FROM private_article_drafts WHERE owner=? AND draft_id=?", arguments: [owner, id]) else { throw SocialDraftStorageError.notFound }
    let deleted: Bool = row["deleted"]
    let payload: ArticleDraftPayload?
    if deleted { payload = nil }
    else {
      guard let bytes: Data = row["payload"] else { throw SocialDraftStorageError.unavailable }
      do { payload = try JSONDecoder().decode(ArticleDraftPayload.self, from: cipher.openMetadata(bytes, owner: owner, assetID: "article-draft:" + id)) }
      catch { throw SocialDraftStorageError.unavailable }
    }
    var attachments: [ArticleDraftAssetBinding] = []
    if !deleted {
      for binding in try Row.fetchAll(db, sql: "SELECT local_asset_id,asset_id FROM private_article_draft_bindings WHERE owner=? AND draft_id=? ORDER BY local_asset_id", arguments: [owner, id]) {
        let assetID: String = binding["asset_id"]
        let (value, _) = try asset(owner: owner, id: assetID, in: db)
        attachments.append(.init(localAssetId: binding["local_asset_id"], asset: value))
      }
    }
    let updated: Double = row["updated_at"]
    return .init(draftId: id, revision: row["revision"], deleted: deleted, mutationId: row["mutation_id"], updatedAt: ISO8601DateFormatter().string(from: Date(timeIntervalSince1970: updated)), draft: payload, attachments: attachments)
  }

  func articleDraft(owner: String, id: String) async throws -> ArticleDraftDocument {
    guard SocialDraftValidation.owner(owner), ArticleDraftValidation.identifier(id) else { throw SocialDraftStorageError.invalid }
    let lease = try admission.acquire(owner: owner)
    defer { withExtendedLifetime(lease) {} }
    return try await database.read { try articleDraft(owner: owner, id: id, in: $0) }
  }

  func articleDrafts(owner: String, cursor: String?, limit: Int) async throws -> ArticleDraftPage {
    guard SocialDraftValidation.owner(owner), (1...20).contains(limit), cursor.map(ArticleDraftValidation.identifier) ?? true else { throw SocialDraftStorageError.invalid }
    let lease = try admission.acquire(owner: owner)
    defer { withExtendedLifetime(lease) {} }
    return try await database.read { db in
      let ids = try String.fetchAll(db, sql: "SELECT draft_id FROM private_article_drafts WHERE owner=? AND (? IS NULL OR draft_id>?) ORDER BY draft_id LIMIT ?", arguments: [owner, cursor, cursor, limit + 1])
      let documents = try ids.prefix(limit).map { try articleDraft(owner: owner, id: $0, in: db) }
      return .init(drafts: documents, cursor: ids.count > limit ? documents.last?.draftId : nil)
    }
  }
}
