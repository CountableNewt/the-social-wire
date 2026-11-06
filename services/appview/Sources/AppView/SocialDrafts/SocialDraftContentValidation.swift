import Foundation

enum SocialDraftContentValidation {
  static func validate(file: SocialDraftTemporaryFile, asset: SocialDraftAsset) throws {
    let input = try FileHandle(forReadingFrom: file.url)
    defer { try? input.close() }
    if asset.contentType == "text/vtt" { try caption(input); return }
    let header = Array(try input.read(upToCount: 512) ?? Data())
    let type: String?
    if header.starts(with: [0xff, 0xd8, 0xff]) { type = "image/jpeg" }
    else if header.starts(with: [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]) { type = "image/png" }
    else if header.starts(with: Array("GIF87a".utf8)) || header.starts(with: Array("GIF89a".utf8)) { type = "image/gif" }
    else if header.count >= 14, header.starts(with: Array("RIFF".utf8)), Array(header[8..<14]) == Array("WEBPVP".utf8) { type = "image/webp" }
    else if header.starts(with: [0x1a, 0x45, 0xdf, 0xa3]) { type = "video/webm" }
    else if mp4(header) { type = "video/mp4" }
    else { type = nil }
    guard type == asset.contentType else { throw SocialDraftStorageError.invalid }
  }

  private static func mp4(_ bytes: [UInt8]) -> Bool {
    guard bytes.count >= 12, Array(bytes[4..<8]) == Array("ftyp".utf8) else { return false }
    let length = bytes.prefix(4).reduce(0) { ($0 << 8) | Int($1) }
    guard length >= 12, length <= bytes.count, length % 4 == 0 else { return false }
    for offset in stride(from: 8, to: length, by: 4) where offset != 12 {
      if Array(bytes[offset..<offset + 3]) == Array("mp4".utf8) { return true }
    }
    return false
  }

  private static func caption(_ input: FileHandle) throws {
    var pending = Data()
    var first = true
    func validateLine(_ input: Data) throws {
      var line = input
      if line.last == 13 { line.removeLast() }
      guard line.count < 1 << 20, var text = String(data: line, encoding: .utf8),
        !line.contains(where: { $0 < 32 && $0 != 9 }) else { throw SocialDraftStorageError.invalid }
      if first {
        if text.hasPrefix("\u{feff}") { text.removeFirst() }
        guard text == "WEBVTT" || text.hasPrefix("WEBVTT ") || text.hasPrefix("WEBVTT\t") else {
          throw SocialDraftStorageError.invalid
        }
        first = false
      }
    }
    while let next = try input.read(upToCount: 64 << 10), !next.isEmpty {
      pending.append(next)
      while let newline = pending.firstIndex(of: 10) {
        try validateLine(Data(pending[..<newline]))
        pending = Data(pending[pending.index(after: newline)...])
      }
      guard pending.count < 1 << 20 else { throw SocialDraftStorageError.invalid }
    }
    if !pending.isEmpty { try validateLine(pending) }
    guard !first else { throw SocialDraftStorageError.invalid }
  }
}
