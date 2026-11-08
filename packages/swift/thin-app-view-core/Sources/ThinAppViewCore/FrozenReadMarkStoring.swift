import Foundation

/// Applies an earlier frozen selection without replacing subsequent explicit unread intent.
public protocol FrozenReadMarkStoring: Sendable {
  func applyFrozenReadMarks(viewerDid: String, subjectUris: [String], createdAt: Date) async throws -> [String]
}
