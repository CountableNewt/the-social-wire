import Foundation
import PostgresNIO

extension PostgresThinAppViewStore: ContentVerificationMetadataStoring {
  public func contentVerificationMetadata(uri: String) async throws -> ContentVerificationMetadata? {
    let rows = try await pool.query("SELECT cid, publication_site FROM content_items WHERE uri=\(uri) AND expires_at > \(Date()) LIMIT 1", logger: logger)
    for try await row in rows {
      let (cid, site) = try row.decode((String, String?).self)
      return ContentVerificationMetadata(cid: cid, publicationSite: site)
    }
    return nil
  }

  public func upsertContentItem(_ item: IndexedContentItem, expectedCID: String?) async throws -> Bool {
    let render = try item.render.encodedJSON()
    let query: PostgresQuery
    if let expectedCID {
      query = """
        UPDATE content_items SET cid=\(item.cid), author_did=\(item.authorDid), collection=\(item.collection),
          created_at=\(item.createdAt), indexed_at=\(item.indexedAt), publication_site=\(item.publicationSite),
          render_json=\(render)::jsonb, expires_at=\(item.expiresAt)
        WHERE uri=\(item.uri) AND cid=\(expectedCID) RETURNING uri
        """
    } else {
      query = """
        INSERT INTO content_items(uri,cid,author_did,collection,created_at,indexed_at,publication_site,render_json,expires_at)
        VALUES (\(item.uri),\(item.cid),\(item.authorDid),\(item.collection),\(item.createdAt),\(item.indexedAt),\(item.publicationSite),\(render)::jsonb,\(item.expiresAt))
        ON CONFLICT(uri) DO NOTHING RETURNING uri
        """
    }
    try Task.checkCancellation()
    let rows = try await pool.query(query, logger: logger)
    for try await _ in rows { return true }
    return false
  }

  public func deleteContentItem(uri: String, expectedCID: String) async throws -> Bool {
    let rows = try await pool.query("DELETE FROM content_items WHERE uri=\(uri) AND cid=\(expectedCID) RETURNING uri", logger: logger)
    for try await _ in rows { return true }
    return false
  }
}
