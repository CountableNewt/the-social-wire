public protocol AuthoritativeContentVerifying: Sendable {
  /// False means an explicit RecordNotFound from the current resolved PDS was applied as a deletion.
  /// Provider absence, malformed responses and transient failures throw instead.
  func verify(entryURI: String) async throws -> Bool
}
