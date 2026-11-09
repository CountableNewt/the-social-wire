import Crypto
import Foundation
import GRDB

extension SQLiteSocialDraftService {
  func putArticleDraft(owner: String, input: ArticleDraftPutInput) async throws -> ArticleDraftDocument {
    guard SocialDraftValidation.owner(owner), ArticleDraftValidation.revision(input.expectedRevision), ArticleDraftValidation.identifier(input.mutationId), ArticleDraftValidation.payload(input.draft), ArticleDraftValidation.references(input.attachments, draft: input.draft) else { throw SocialDraftStorageError.invalid }
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
    let clear = try encoder.encode(input.draft)
    guard clear.count <= ArticleDraftValidation.maximumPayloadBytes else { throw SocialDraftStorageError.quota }
    let fingerprint = Data(SHA256.hash(data: try encoder.encode(input)))
    return try mutateArticleDraft(owner: owner, id: input.draft.id, expectedRevision: input.expectedRevision, mutationID: input.mutationId, fingerprint: fingerprint, payload: input.draft, clear: clear, references: input.attachments)
  }

  func deleteArticleDraft(owner: String, input: ArticleDraftDeleteInput) async throws -> ArticleDraftDocument {
    guard SocialDraftValidation.owner(owner), ArticleDraftValidation.revision(input.expectedRevision), ArticleDraftValidation.identifier(input.mutationId), ArticleDraftValidation.identifier(input.draftId) else { throw SocialDraftStorageError.invalid }
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
    let fingerprint = Data(SHA256.hash(data: try encoder.encode(input)))
    return try mutateArticleDraft(owner: owner, id: input.draftId, expectedRevision: input.expectedRevision, mutationID: input.mutationId, fingerprint: fingerprint, payload: nil, clear: nil, references: [])
  }

  private func mutateArticleDraft(owner: String, id: String, expectedRevision: Int64, mutationID: String, fingerprint: Data, payload: ArticleDraftPayload?, clear: Data?, references: [ArticleDraftAttachmentReference]) throws -> ArticleDraftDocument {
    let lease = try admission.acquire(owner: owner)
    defer { withExtendedLifetime(lease) {} }
    return try database.write { db in
      let previous: ArticleDraftDocument?
      do { previous = try articleDraft(owner: owner, id: id, in: db) }
      catch SocialDraftStorageError.notFound { previous = nil }
      if let previous, previous.mutationId == mutationID {
        guard try Data.fetchOne(db, sql: "SELECT mutation_hash FROM private_article_drafts WHERE owner=? AND draft_id=?", arguments: [owner, id]) == fingerprint else { throw SocialDraftStorageError.conflict }
        return previous
      }
      guard previous?.deleted != true, (previous?.revision ?? 0) == expectedRevision else { throw SocialDraftStorageError.conflict }
      if let old = previous?.draft, let payload {
        if let published = old.publishedUri, payload.publishedUri != published { throw SocialDraftStorageError.conflict }
        if old.isLocked {
          if let claim = old.publicationClaimId, payload.isLocked, payload.publicationClaimId != claim { throw SocialDraftStorageError.conflict }
          let oldRefs = previous!.attachments.map { ArticleDraftAttachmentReference(localAssetId: $0.localAssetId, assetId: $0.asset.assetId) }
          guard old.hasSameContent(as: payload), oldRefs.sorted(by: { $0.localAssetId < $1.localAssetId }) == references.sorted(by: { $0.localAssetId < $1.localAssetId }) else { throw SocialDraftStorageError.conflict }
        }
      }
      if payload != nil || previous == nil {
        let quota = try Row.fetchOne(db, sql: "SELECT count(*) AS total,coalesce(sum(CASE WHEN deleted=0 THEN 1 ELSE 0 END),0) AS live,coalesce(sum(payload_bytes),0) AS used FROM private_article_drafts WHERE owner=?", arguments: [owner])!
        let total: Int = quota["total"], live: Int = quota["live"], used: Int64 = quota["used"]
        let oldBytes = try Int64.fetchOne(db, sql: "SELECT payload_bytes FROM private_article_drafts WHERE owner=? AND draft_id=?", arguments: [owner, id]) ?? 0
        guard previous != nil || (total < 1000 && (payload == nil || live < 200)), used - oldBytes + Int64(clear?.count ?? 0) <= 32 << 20 else { throw SocialDraftStorageError.quota }
      }
      for ref in references {
        let (value, _) = try asset(owner: owner, id: ref.assetId, in: db)
        guard value.state == "ready" else { throw SocialDraftStorageError.uploadPending }
        guard let descriptor = payload?.assets.first(where: { $0.id == ref.localAssetId }), value.contentType == descriptor.mimeType, value.name == descriptor.name, value.kind == "photo", value.byteLength <= 1_000_000 else { throw SocialDraftStorageError.invalid }
      }
      let encrypted = try clear.map { try cipher.sealMetadata($0, owner: owner, assetID: "article-draft:" + id) }
      try db.execute(sql: "INSERT INTO private_social_draft_accounts(owner) VALUES(?) ON CONFLICT DO NOTHING", arguments: [owner])
      try db.execute(sql: """
        INSERT INTO private_article_drafts(owner,draft_id,revision,deleted,mutation_id,mutation_hash,payload,payload_bytes,updated_at)
        VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(owner,draft_id) DO UPDATE SET
        revision=excluded.revision,deleted=excluded.deleted,mutation_id=excluded.mutation_id,mutation_hash=excluded.mutation_hash,
        payload=excluded.payload,payload_bytes=excluded.payload_bytes,updated_at=excluded.updated_at
        """, arguments: [owner, id, expectedRevision + 1, payload == nil, mutationID, fingerprint, encrypted, clear?.count ?? 0, Date().timeIntervalSince1970])
      try db.execute(sql: "DELETE FROM private_article_draft_bindings WHERE owner=? AND draft_id=?", arguments: [owner, id])
      for ref in references {
        try db.execute(sql: "INSERT INTO private_article_draft_bindings(owner,draft_id,local_asset_id,asset_id) VALUES(?,?,?,?)", arguments: [owner, id, ref.localAssetId, ref.assetId])
        try db.execute(sql: "DELETE FROM private_social_draft_cleanup WHERE owner=? AND asset_id=?", arguments: [owner, ref.assetId])
      }
      for old in previous?.attachments ?? [] { try retireIfUnreferenced(owner: owner, id: old.asset.assetId, in: db) }
      return try articleDraft(owner: owner, id: id, in: db)
    }
  }
}
