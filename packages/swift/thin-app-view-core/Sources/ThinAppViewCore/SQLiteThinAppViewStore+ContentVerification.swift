import Foundation
@preconcurrency import GRDB

extension SQLiteThinAppViewStore: ContentVerificationMetadataStoring {
  public func contentVerificationMetadata(uri: String) async throws -> ContentVerificationMetadata? {
    let now = ISO8601DateFormatter().string(from: Date())
    return try await db.read { db in
      guard let row = try Row.fetchOne(db, sql: "SELECT cid, publication_site FROM content_items WHERE uri=? AND expires_at > ? LIMIT 1", arguments: [uri, now]) else { return nil }
      return ContentVerificationMetadata(cid: row["cid"], publicationSite: row["publication_site"])
    }
  }

  public func upsertContentItem(_ item: IndexedContentItem, expectedCID: String?) async throws -> Bool {
    let render = try item.render.encodedJSON()
    let createdAt = ISO8601DateFormatter().string(from: item.createdAt)
    let indexedAt = ISO8601DateFormatter().string(from: item.indexedAt)
    let expiresAt = ISO8601DateFormatter().string(from: item.expiresAt)
    return try await db.write { db in
      try Task.checkCancellation()
      if let expectedCID {
        try db.execute(sql: """
          UPDATE content_items SET cid=?,author_did=?,collection=?,created_at=?,indexed_at=?,publication_site=?,render_json=?,expires_at=?
          WHERE uri=? AND cid=?
          """, arguments: [item.cid,item.authorDid,item.collection,createdAt,indexedAt,item.publicationSite,render,expiresAt,item.uri,expectedCID])
      } else {
        try db.execute(sql: """
          INSERT INTO content_items(uri,cid,author_did,collection,created_at,indexed_at,publication_site,render_json,expires_at)
          VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(uri) DO NOTHING
          """, arguments: [item.uri,item.cid,item.authorDid,item.collection,createdAt,indexedAt,item.publicationSite,render,expiresAt])
      }
      return db.changesCount > 0
    }
  }

  public func deleteContentItem(uri: String, expectedCID: String) async throws -> Bool {
    try await db.write { db in
      try db.execute(sql: "DELETE FROM content_items WHERE uri=? AND cid=?", arguments: [uri, expectedCID])
      return db.changesCount > 0
    }
  }
}
