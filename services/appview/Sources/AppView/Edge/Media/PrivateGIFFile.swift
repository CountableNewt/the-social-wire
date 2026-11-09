import Foundation

/// Owns the private spool even when a response is discarded before its body runs.
final class PrivateGIFFile: Sendable {
  let directory: URL
  let url: URL
  let length: Int
  let mime: String

  init(directory: URL, url: URL, length: Int, mime: String) {
    self.directory = directory
    self.url = url
    self.length = length
    self.mime = mime
  }

  deinit { try? FileManager.default.removeItem(at: directory) }
}
