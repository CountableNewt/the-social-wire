import Foundation

struct RepositoryBinaryCursor {
  let bytes: [UInt8]
  var offset = 0
  init(_ data: Data) { bytes = Array(data) }
  var remaining: Int { bytes.count - offset }
  mutating func read(_ count: Int) throws -> Data {
    guard count >= 0, count <= remaining else {
      throw RepositorySnapshotError.invalid("truncated bytes")
    }
    defer { offset += count }
    return Data(bytes[offset..<(offset + count)])
  }
  mutating func integer(_ count: Int) throws -> UInt64 {
    try read(count).reduce(0) { ($0 << 8) | UInt64($1) }
  }
  mutating func varint() throws -> Int {
    let start = offset
    var value: UInt64 = 0
    for shift in stride(from: 0, to: 64, by: 7) {
      let byte = try integer(1)
      guard shift < 63 || byte <= 1 else {
        throw RepositorySnapshotError.invalid("varint overflow")
      }
      value |= (byte & 127) << shift
      if byte & 128 == 0 {
        guard value <= UInt64(Int.max), offset - start == 1 || byte != 0 else {
          throw RepositorySnapshotError.invalid("noncanonical varint")
        }
        return Int(value)
      }
    }
    throw RepositorySnapshotError.invalid("varint overflow")
  }
}
