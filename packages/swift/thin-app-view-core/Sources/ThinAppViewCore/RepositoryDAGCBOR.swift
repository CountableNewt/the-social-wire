import Foundation

enum RepositoryDAGCBOR {
  static func decode(_ data: Data) throws -> RepositoryCBORValue {
    var reader = RepositoryBinaryCursor(data)
    let value = try decode(&reader, depth: 0)
    guard reader.remaining == 0, encode(value) == data else {
      throw RepositorySnapshotError.invalid("noncanonical DAG-CBOR")
    }
    return value
  }

  static func encode(_ value: RepositoryCBORValue) -> Data {
    var bytes = Data()
    switch value {
    case .map(let value):
      header(5, UInt64(value.count), into: &bytes)
      let keys = value.keys.sorted {
        $0.utf8.count == $1.utf8.count
          ? $0.utf8.lexicographicallyPrecedes($1.utf8) : $0.utf8.count < $1.utf8.count
      }
      for key in keys {
        bytes += encode(.string(key))
        bytes += encode(value[key]!)
      }
    case .array(let value):
      header(4, UInt64(value.count), into: &bytes)
      for item in value { bytes += encode(item) }
    case .string(let value):
      let data = Data(value.utf8)
      header(3, UInt64(data.count), into: &bytes)
      bytes += data
    case .bytes(let value):
      header(2, UInt64(value.count), into: &bytes)
      bytes += value
    case .link(let value):
      header(6, 42, into: &bytes)
      bytes += encode(.bytes(Data([0]) + value.bytes))
    case .unsigned(let value): header(0, value, into: &bytes)
    case .negative(let value): header(1, value, into: &bytes)
    case .double(let value):
      header(7, 27, into: &bytes, fixed: true)
      append(value.bitPattern, width: 8, into: &bytes)
    case .bool(let value): bytes.append(value ? 0xf5 : 0xf4)
    case .null: bytes.append(0xf6)
    }
    return bytes
  }

  private static func decode(_ reader: inout RepositoryBinaryCursor, depth: Int) throws
    -> RepositoryCBORValue
  {
    guard depth <= 128 else { throw RepositorySnapshotError.invalid("DAG-CBOR nesting exceeded") }
    let head = try reader.integer(1)
    let major = head >> 5
    let additional = head & 31
    if major == 7 {
      switch additional {
      case 20: return .bool(false)
      case 21: return .bool(true)
      case 22: return .null
      case 27:
        let value = Double(bitPattern: try reader.integer(8))
        guard value.isFinite else { throw RepositorySnapshotError.invalid("nonfinite number") }
        return .double(value)
      default: throw RepositorySnapshotError.invalid("invalid CBOR simple value")
      }
    }
    let length: UInt64
    switch additional {
    case 0...23: length = additional
    case 24: length = try reader.integer(1)
    case 25: length = try reader.integer(2)
    case 26: length = try reader.integer(4)
    case 27: length = try reader.integer(8)
    default: throw RepositorySnapshotError.invalid("indefinite CBOR")
    }
    switch major {
    case 0: return .unsigned(length)
    case 1:
      guard length <= UInt64(Int64.max) else {
        throw RepositorySnapshotError.invalid("negative integer overflow")
      }
      return .negative(length)
    case 2, 3:
      guard length <= UInt64(reader.remaining) else {
        throw RepositorySnapshotError.invalid("truncated CBOR")
      }
      let bytes = try reader.read(Int(length))
      if major == 2 { return .bytes(bytes) }
      guard let text = String(data: bytes, encoding: .utf8) else {
        throw RepositorySnapshotError.invalid("invalid UTF8")
      }
      return .string(text)
    case 4:
      guard length <= UInt64(reader.remaining) else {
        throw RepositorySnapshotError.invalid("array length exceeded")
      }
      var values: [RepositoryCBORValue] = []
      for _ in 0..<Int(length) { values.append(try decode(&reader, depth: depth + 1)) }
      return .array(values)
    case 5:
      guard length <= UInt64(reader.remaining / 2) else {
        throw RepositorySnapshotError.invalid("map length exceeded")
      }
      var values: [String: RepositoryCBORValue] = [:]
      for _ in 0..<Int(length) {
        guard case .string(let key) = try decode(&reader, depth: depth + 1), values[key] == nil
        else {
          throw RepositorySnapshotError.invalid("duplicate or invalid map key")
        }
        values[key] = try decode(&reader, depth: depth + 1)
      }
      return .map(values)
    case 6:
      guard length == 42, case .bytes(let bytes) = try decode(&reader, depth: depth + 1),
        bytes.first == 0
      else {
        throw RepositorySnapshotError.invalid("invalid CBOR link")
      }
      return .link(try RepositoryCID(bytes: bytes.dropFirst()))
    default: throw RepositorySnapshotError.invalid("unsupported DAG-CBOR value")
    }
  }

  private static func header(
    _ major: UInt8, _ value: UInt64, into bytes: inout Data, fixed: Bool = false
  ) {
    if fixed {
      bytes.append(major << 5 | UInt8(value))
      return
    }
    if value < 24 {
      bytes.append(major << 5 | UInt8(value))
      return
    }
    let width = value <= 255 ? 1 : value <= 65_535 ? 2 : value <= 4_294_967_295 ? 4 : 8
    bytes.append(major << 5 | (width == 1 ? 24 : width == 2 ? 25 : width == 4 ? 26 : 27))
    append(value, width: width, into: &bytes)
  }

  private static func append(_ value: UInt64, width: Int, into bytes: inout Data) {
    for shift in stride(from: (width - 1) * 8, through: 0, by: -8) {
      bytes.append(UInt8(truncatingIfNeeded: value >> shift))
    }
  }
}
