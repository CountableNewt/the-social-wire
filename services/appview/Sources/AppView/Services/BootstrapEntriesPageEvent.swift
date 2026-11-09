import Foundation
import GatewayCore
import ThinAppViewCore

/// AppView-owned additive entry metadata keeps the pinned GatewayCore envelope compatible.
struct BootstrapEntriesPageEvent: Encodable {
  let publicationId: String
  let page: AppViewEntryListResponse
  let source: AppViewBootstrapEvidenceSource
  var cachedAt: Date? = nil
  var expiresAt: Date? = nil

  private enum Key: String, CodingKey {
    case kind, entriesPage, publicationId, entries, cursor, source, cachedAt, expiresAt
    case entryId, title, summary, publishedAt, thumbnailUrl, thumbnailFallbackUrl, originalUrl, contentLabels
  }

  func encode(to encoder: any Encoder) throws {
    var event = encoder.container(keyedBy: Key.self)
    try event.encode("entriesPage", forKey: .kind)
    var payload = event.nestedContainer(keyedBy: Key.self, forKey: .entriesPage)
    try payload.encode(publicationId, forKey: .publicationId)
    try payload.encodeIfPresent(page.cursor, forKey: .cursor)
    try payload.encode(source, forKey: .source)
    try payload.encodeIfPresent(cachedAt, forKey: .cachedAt)
    try payload.encodeIfPresent(expiresAt, forKey: .expiresAt)
    var entries = payload.nestedUnkeyedContainer(forKey: .entries)
    for item in page.entries {
      var row = entries.nestedContainer(keyedBy: Key.self)
      try row.encode(item.entryId, forKey: .entryId)
      try row.encode(item.title, forKey: .title)
      try row.encodeIfPresent(item.summary, forKey: .summary)
      try row.encode(item.publishedAt, forKey: .publishedAt)
      try row.encodeIfPresent(item.thumbnailUrl, forKey: .thumbnailUrl)
      try row.encodeIfPresent(item.thumbnailFallbackUrl, forKey: .thumbnailFallbackUrl)
      try row.encodeIfPresent(item.originalUrl, forKey: .originalUrl)
      try row.encodeIfPresent(item.contentLabels, forKey: .contentLabels)
    }
  }

  func encodedLine() throws -> Data {
    let encoder = JSONEncoder()
    encoder.dateEncodingStrategy = .iso8601
    var data = try encoder.encode(self)
    data.append(0x0A)
    return data
  }
}
