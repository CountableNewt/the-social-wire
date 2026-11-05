import Foundation

struct BinaryCursor {
  let bytes: [UInt8]
  var offset = 0
  init(_ data: Data) { bytes = Array(data) }
  var remaining: Int { bytes.count - offset }
  mutating func read(_ count: Int) throws -> [UInt8] {
    guard count >= 0, count <= remaining else {
      throw IngressError.malformedArchive("Truncated binary data")
    }
    defer { offset += count }
    return Array(bytes[offset..<(offset + count)])
  }
  mutating func little(_ count: Int) throws -> UInt64 {
    try read(count).enumerated().reduce(UInt64(0)) { $0 | UInt64($1.element) << ($1.offset * 8) }
  }
  mutating func big(_ count: Int) throws -> UInt64 {
    try read(count).reduce(UInt64(0)) { ($0 << 8) | UInt64($1) }
  }
  mutating func text(_ count: Int) throws -> String {
    guard let value = String(bytes: try read(count), encoding: .utf8) else {
      throw IngressError.malformedArchive("Invalid UTF-8")
    }
    return value
  }
}
