import Foundation
import PostgresNIO

extension SocialDraftService {
  func manifest(owner: String, id: String, connection: PostgresConnection) async throws -> SocialDraftManifest {
    let rows = try await connection.query("""
      SELECT revision,deleted FROM private_social_draft_manifests WHERE owner=\(owner) AND draft_id=\(id)
      """, logger: logger)
    var manifest: SocialDraftManifest?
    for try await row in rows {
      let (revision, deleted) = try row.decode((Int64, Bool).self)
      manifest = .init(draftId: id, revision: revision, deleted: deleted, attachments: [])
    }
    guard var result = manifest else { throw SocialDraftStorageError.notFound }
    guard !result.deleted else { return result }
    let bindings = try await connection.query("""
      SELECT asset_id::text,role,ref FROM private_social_draft_bindings
      WHERE owner=\(owner) AND draft_id=\(id) ORDER BY position
      """, logger: logger)
    var references: [SocialDraftManifestReference] = []
    for try await row in bindings {
      let (assetId, role, ref) = try row.decode((String, String, String).self)
      references.append(.init(assetId: assetId, role: role, ref: ref))
    }
    for reference in references {
      let (asset, _) = try await self.asset(owner: owner, id: reference.assetId, connection: connection)
      result.attachments.append(.init(assetId: reference.assetId, role: reference.role, ref: reference.ref, asset: asset))
    }
    return result
  }

  func manifest(owner: String, id: String) async throws -> SocialDraftManifest {
    guard SocialDraftValidation.draftID(id), SocialDraftValidation.owner(owner) else { throw SocialDraftStorageError.invalid }
    let lease = try admission.acquire(owner: owner)
    defer { withExtendedLifetime(lease) {} }
    return try await withOwner(owner) { connection in try await self.manifest(owner: owner, id: id, connection: connection) }
  }

  func putManifest(owner: String, input: SocialDraftPutManifestInput, deleted: Bool = false) async throws -> SocialDraftManifest {
    guard SocialDraftValidation.owner(owner), SocialDraftValidation.draftID(input.draftId),
      input.expectedRevision >= 0, input.expectedRevision < Int64.max,
      SocialDraftValidation.references(input.attachments) else { throw SocialDraftStorageError.invalid }
    let lease = try admission.acquire(owner: owner)
    defer { withExtendedLifetime(lease) {} }
    return try await withOwner(owner) { connection in
      let previous: SocialDraftManifest
      do { previous = try await self.manifest(owner: owner, id: input.draftId, connection: connection) }
      catch SocialDraftStorageError.notFound {
        previous = .init(draftId: input.draftId, revision: 0, deleted: false, attachments: [])
      }
      guard previous.revision == input.expectedRevision else { throw SocialDraftStorageError.conflict }
      for ref in input.attachments {
        let (asset, _) = try await self.asset(owner: owner, id: ref.assetId, connection: connection)
        guard asset.state == "ready" else { throw SocialDraftStorageError.uploadPending }
      }
      try await connection.query("""
        INSERT INTO private_social_draft_manifests(owner,draft_id,revision,deleted)
        VALUES(\(owner),\(input.draftId),\(input.expectedRevision + 1),\(deleted))
        ON CONFLICT(owner,draft_id) DO UPDATE SET revision=EXCLUDED.revision,deleted=EXCLUDED.deleted
        """, logger: self.logger)
      try await connection.query("DELETE FROM private_social_draft_bindings WHERE owner=\(owner) AND draft_id=\(input.draftId)", logger: self.logger)
      for (position, ref) in input.attachments.enumerated() {
        try await connection.query("""
          INSERT INTO private_social_draft_bindings(owner,draft_id,position,asset_id,role,ref)
          VALUES(\(owner),\(input.draftId),\(position),\(ref.assetId)::uuid,\(ref.role),\(ref.ref))
          """, logger: self.logger)
        try await connection.query("DELETE FROM private_social_draft_cleanup WHERE owner=\(owner) AND asset_id=\(ref.assetId)::uuid", logger: self.logger)
      }
      for old in previous.attachments {
        let references = try await connection.query("""
          SELECT EXISTS(SELECT 1 FROM private_social_draft_bindings WHERE owner=\(owner) AND asset_id=\(old.assetId)::uuid)
          """, logger: self.logger)
        var referenced = false
        for try await row in references { referenced = try row.decode(Bool.self) }
        if !referenced {
          try await connection.query("""
            UPDATE private_social_draft_assets SET state='deleted' WHERE owner=\(owner) AND asset_id=\(old.assetId)::uuid
            """, logger: self.logger)
          try await connection.query("""
            INSERT INTO private_social_draft_cleanup(owner,asset_id,available_at) VALUES(\(owner),\(old.assetId)::uuid,now())
            ON CONFLICT(owner,asset_id) DO UPDATE SET available_at=now(),attempts=0
            """, logger: self.logger)
        }
      }
      return try await self.manifest(owner: owner, id: input.draftId, connection: connection)
    }
  }
}
