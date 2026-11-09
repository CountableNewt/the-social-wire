/// Provider hints may create a derived row, but can never replace an existing
/// version observed from the authoritative PDS. Implementations must be atomic.
public protocol PublicContentInsertStoring: Sendable {
  func insertContentItemIfAbsent(_ item: IndexedContentItem) async throws -> Bool
}

extension SQLiteThinAppViewStore: PublicContentInsertStoring {}
extension PostgresThinAppViewStore: PublicContentInsertStoring {}
