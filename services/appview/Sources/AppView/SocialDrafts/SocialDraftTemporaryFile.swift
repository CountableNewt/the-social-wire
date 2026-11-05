import Foundation
#if canImport(Darwin)
import Darwin
#else
import Glibc
#endif

/// mkstemp creates an exclusive 0600 file. Immutable ownership allows safe transfer to a response body.
final class SocialDraftTemporaryFile: Sendable {
  let url: URL
  init() throws {
    var pattern = Array(FileManager.default.temporaryDirectory.appendingPathComponent("social-draft-XXXXXX").path.utf8CString)
    let descriptor = mkstemp(&pattern)
    guard descriptor >= 0 else { throw SocialDraftStorageError.unavailable }
    close(descriptor)
    self.url = URL(fileURLWithPath: String(decoding: pattern.dropLast().map { UInt8(bitPattern: $0) }, as: UTF8.self))
  }
  deinit { try? FileManager.default.removeItem(at: url) }
}
