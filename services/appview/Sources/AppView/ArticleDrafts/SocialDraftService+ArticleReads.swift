import Foundation
import NIOCore
import PostgresNIO

extension SocialDraftService {
  func articleDraft(owner: String, id: String, connection: PostgresConnection) async throws -> ArticleDraftDocument {
    let rows = try await connection.query("""
      SELECT revision,deleted,mutation_id::text,updated_at,payload FROM private_article_drafts
      WHERE owner=\(owner) AND draft_id=\(id)::uuid
      """, logger: logger)
    for try await row in rows {
      let (revision, deleted, mutation, updatedAt, bytes) = try row.decode((Int64, Bool, String, Date, ByteBuffer?).self)
      let payload: ArticleDraftPayload?
      if deleted { payload = nil }
      else {
        guard let bytes else { throw SocialDraftStorageError.unavailable }
        do { payload = try JSONDecoder().decode(ArticleDraftPayload.self,
          from: cipher.openMetadata(Data(buffer: bytes), owner: owner, assetID: "article-draft:" + id)) }
        catch { throw SocialDraftStorageError.unavailable }
      }
      var attachments: [ArticleDraftAssetBinding] = []
      if !deleted {
        let bindings = try await connection.query("""
          SELECT local_asset_id::text,asset_id::text FROM private_article_draft_bindings
          WHERE owner=\(owner) AND draft_id=\(id)::uuid ORDER BY local_asset_id
          """, logger: logger)
        var refs: [(String, String)] = []
        for try await binding in bindings { refs.append(try binding.decode((String, String).self)) }
        for (localID, assetID) in refs {
          let (asset, _) = try await self.asset(owner: owner, id: assetID, connection: connection)
          attachments.append(.init(localAssetId: localID, asset: asset))
        }
      }
      return .init(draftId: id, revision: revision, deleted: deleted, mutationId: mutation,
        updatedAt: ISO8601DateFormatter().string(from: updatedAt), draft: payload, attachments: attachments)
    }
    throw SocialDraftStorageError.notFound
  }

  func articleDraft(owner: String, id: String) async throws -> ArticleDraftDocument {
    guard SocialDraftValidation.owner(owner), ArticleDraftValidation.identifier(id) else { throw SocialDraftStorageError.invalid }
    let lease = try admission.acquire(owner: owner)
    defer { withExtendedLifetime(lease) {} }
    return try await withOwner(owner) { connection in try await self.articleDraft(owner: owner, id: id, connection: connection) }
  }

  func articleDrafts(owner: String, cursor: String?, limit: Int) async throws -> ArticleDraftPage {
    guard SocialDraftValidation.owner(owner), (1...20).contains(limit),
      cursor.map(ArticleDraftValidation.identifier) ?? true else { throw SocialDraftStorageError.invalid }
    let lease = try admission.acquire(owner: owner)
    defer { withExtendedLifetime(lease) {} }
    return try await withOwner(owner) { connection in
      let rows = try await connection.query("""
        SELECT draft_id::text FROM private_article_drafts WHERE owner=\(owner)
        AND (\(cursor)::text IS NULL OR draft_id>\(cursor)::uuid) ORDER BY draft_id LIMIT \(limit + 1)
        """, logger: self.logger)
      var ids: [String] = []
      for try await row in rows { ids.append(try row.decode(String.self)) }
      let more = ids.count > limit
      var documents: [ArticleDraftDocument] = []
      for id in ids.prefix(limit) { documents.append(try await self.articleDraft(owner: owner, id: id, connection: connection)) }
      return .init(drafts: documents, cursor: more ? documents.last?.draftId : nil)
    }
  }
}
