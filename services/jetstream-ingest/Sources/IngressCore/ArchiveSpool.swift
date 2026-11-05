import Foundation

/// Ownership survives task-group failures so successful sibling downloads cannot leak spool files.
final class ArchiveSpool: Sendable {
  let url: URL
  init(_ url: URL) { self.url = url }
  deinit { try? FileManager.default.removeItem(at: url) }
}
