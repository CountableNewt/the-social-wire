import Crypto
import Foundation
import NIOCore
import PostgresNIO

extension SocialDraftService {
  func putArticleDraft(owner: String, input: ArticleDraftPutInput) async throws -> ArticleDraftDocument {
    guard SocialDraftValidation.owner(owner), ArticleDraftValidation.revision(input.expectedRevision),
      ArticleDraftValidation.identifier(input.mutationId), ArticleDraftValidation.payload(input.draft),
      ArticleDraftValidation.references(input.attachments, draft: input.draft) else { throw SocialDraftStorageError.invalid }
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
    let clear = try encoder.encode(input.draft)
    guard clear.count <= ArticleDraftValidation.maximumPayloadBytes else { throw SocialDraftStorageError.quota }
    let fingerprint = Data(SHA256.hash(data: try encoder.encode(input)))
    return try await mutateArticleDraft(owner: owner, id: input.draft.id, expectedRevision: input.expectedRevision,
      mutationID: input.mutationId, fingerprint: fingerprint, payload: input.draft, clear: clear, references: input.attachments)
  }

  func deleteArticleDraft(owner: String, input: ArticleDraftDeleteInput) async throws -> ArticleDraftDocument {
    guard SocialDraftValidation.owner(owner), ArticleDraftValidation.revision(input.expectedRevision),
      ArticleDraftValidation.identifier(input.mutationId), ArticleDraftValidation.identifier(input.draftId) else { throw SocialDraftStorageError.invalid }
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
    let fingerprint = Data(SHA256.hash(data: try encoder.encode(input)))
    return try await mutateArticleDraft(owner: owner, id: input.draftId, expectedRevision: input.expectedRevision,
      mutationID: input.mutationId, fingerprint: fingerprint, payload: nil, clear: nil, references: [])
  }

  private func mutateArticleDraft(owner: String, id: String, expectedRevision: Int64,
    mutationID: String, fingerprint: Data, payload: ArticleDraftPayload?, clear: Data?,
    references: [ArticleDraftAttachmentReference]
  ) async throws -> ArticleDraftDocument {
    let lease = try admission.acquire(owner: owner)
    defer { withExtendedLifetime(lease) {} }
    return try await withOwner(owner) { connection in
      let previous: ArticleDraftDocument?
      do { previous = try await self.articleDraft(owner: owner, id: id, connection: connection) }
      catch SocialDraftStorageError.notFound { previous = nil }
      if let previous, previous.mutationId == mutationID {
        let hashes = try await connection.query("""
          SELECT mutation_hash FROM private_article_drafts WHERE owner=\(owner) AND draft_id=\(id)::uuid
          """, logger: self.logger)
        for try await row in hashes {
          guard Data(buffer: try row.decode(ByteBuffer.self)) == fingerprint else { throw SocialDraftStorageError.conflict }
        }
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
        let quota = try await connection.query("""
          SELECT count(*),count(*) FILTER(WHERE NOT deleted),COALESCE(sum(payload_bytes),0)::bigint
          FROM private_article_drafts WHERE owner=\(owner)
          """, logger: self.logger)
        for try await row in quota {
          let (total, live, used) = try row.decode((Int64, Int64, Int64).self)
          let oldBytes: Int64
          if previous != nil {
            let oldRows = try await connection.query("SELECT payload_bytes FROM private_article_drafts WHERE owner=\(owner) AND draft_id=\(id)::uuid", logger: self.logger)
            var bytes: Int32 = 0
            for try await oldRow in oldRows { bytes = try oldRow.decode(Int32.self) }
            oldBytes = Int64(bytes)
          } else { oldBytes = 0 }
          guard previous != nil || (total < 1000 && (payload == nil || live < 200)), used - oldBytes + Int64(clear?.count ?? 0) <= 32 << 20 else { throw SocialDraftStorageError.quota }
        }
      }
      for reference in references {
        let (asset, _) = try await self.asset(owner: owner, id: reference.assetId, connection: connection)
        guard asset.state == "ready" else { throw SocialDraftStorageError.uploadPending }
        guard let descriptor = payload?.assets.first(where: { $0.id == reference.localAssetId }),
          asset.contentType == descriptor.mimeType, asset.name == descriptor.name,
          asset.kind == "photo", asset.byteLength <= 1_000_000 else { throw SocialDraftStorageError.invalid }
      }
      let encrypted = try clear.map { ByteBuffer(bytes: try self.cipher.sealMetadata($0, owner: owner, assetID: "article-draft:" + id)) }
      try await connection.query("""
        INSERT INTO private_article_drafts(owner,draft_id,revision,deleted,mutation_id,mutation_hash,payload,payload_bytes,updated_at)
        VALUES(\(owner),\(id)::uuid,\(expectedRevision + 1),\(payload == nil),\(mutationID)::uuid,\(ByteBuffer(bytes: fingerprint)),\(encrypted),\(clear?.count ?? 0),now())
        ON CONFLICT(owner,draft_id) DO UPDATE SET revision=EXCLUDED.revision,deleted=EXCLUDED.deleted,
          mutation_id=EXCLUDED.mutation_id,mutation_hash=EXCLUDED.mutation_hash,payload=EXCLUDED.payload,
          payload_bytes=EXCLUDED.payload_bytes,updated_at=EXCLUDED.updated_at
        """, logger: self.logger)
      try await connection.query("DELETE FROM private_article_draft_bindings WHERE owner=\(owner) AND draft_id=\(id)::uuid", logger: self.logger)
      for reference in references {
        try await connection.query("""
          INSERT INTO private_article_draft_bindings(owner,draft_id,local_asset_id,asset_id)
          VALUES(\(owner),\(id)::uuid,\(reference.localAssetId)::uuid,\(reference.assetId)::uuid)
          """, logger: self.logger)
        try await connection.query("DELETE FROM private_social_draft_cleanup WHERE owner=\(owner) AND asset_id=\(reference.assetId)::uuid", logger: self.logger)
      }
      for old in previous?.attachments ?? [] {
        let queries = try await connection.query("""
          SELECT EXISTS(SELECT 1 FROM private_social_draft_bindings WHERE owner=\(owner) AND asset_id=\(old.asset.assetId)::uuid)
            OR EXISTS(SELECT 1 FROM private_article_draft_bindings WHERE owner=\(owner) AND asset_id=\(old.asset.assetId)::uuid)
          """, logger: self.logger)
        var referenced = false
        for try await row in queries { referenced = try row.decode(Bool.self) }
        if !referenced {
          try await connection.query("UPDATE private_social_draft_assets SET state='deleted' WHERE owner=\(owner) AND asset_id=\(old.asset.assetId)::uuid", logger: self.logger)
          try await connection.query("""
            INSERT INTO private_social_draft_cleanup(owner,asset_id,available_at) VALUES(\(owner),\(old.asset.assetId)::uuid,now())
            ON CONFLICT(owner,asset_id) DO UPDATE SET available_at=now(),attempts=0
            """, logger: self.logger)
        }
      }
      return try await self.articleDraft(owner: owner, id: id, connection: connection)
    }
  }
}
