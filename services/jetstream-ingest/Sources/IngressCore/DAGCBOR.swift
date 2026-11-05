import Crypto
import Foundation

/// The bounded DAG-CBOR data model used by V2 archive records. Numbers retain integer precision.
public enum DAGCBOR {
  public static func decode(_ data: Data) throws -> JSONValue {
    var reader = BinaryCursor(data)
    let value = try decode(&reader, depth: 0)
    guard reader.remaining == 0 else {
      throw IngressError.malformedArchive("Trailing DAG-CBOR data")
    }
    return value
  }
  public static func cid(for data: Data) -> String {
    base32([1, 0x71, 0x12, 0x20] + Array(SHA256.hash(data: data)))
  }

  private static func decode(_ reader: inout BinaryCursor, depth: Int) throws -> JSONValue {
    guard depth <= 128 else {
      throw IngressError.malformedArchive("DAG-CBOR nesting exceeds safety limit")
    }
    let byte = try reader.big(1)
    let major = byte >> 5
    let additional = byte & 31
    if major == 7 {
      switch additional {
      case 20: return .bool(false)
      case 21: return .bool(true)
      case 22: return .null
      case 27:
        let value = Double(bitPattern: try reader.big(8))
        guard value.isFinite else {
          throw IngressError.malformedArchive("Nonfinite DAG-CBOR float")
        }
        return .double(value)
      default: throw IngressError.malformedArchive("Unsupported DAG-CBOR simple value")
      }
    }
    let length: UInt64
    switch additional {
    case 0...23: length = additional
    case 24: length = try reader.big(1)
    case 25: length = try reader.big(2)
    case 26: length = try reader.big(4)
    case 27: length = try reader.big(8)
    default: throw IngressError.malformedArchive("Indefinite DAG-CBOR is unsupported")
    }
    switch major {
    case 0: return length <= UInt64(Int64.max) ? .integer(Int64(length)) : .unsigned(length)
    case 1:
      guard length <= UInt64(Int64.max) else {
        throw IngressError.malformedArchive("DAG-CBOR negative integer overflows")
      }
      return .integer(-1 - Int64(length))
    case 2, 3:
      guard length <= UInt64(reader.remaining) else {
        throw IngressError.malformedArchive("DAG-CBOR length exceeds input")
      }
      if major == 3 { return .string(try reader.text(Int(length))) }
      let data = Data(try reader.read(Int(length)))
      return .object([
        "$bytes": .string(data.base64EncodedString().replacingOccurrences(of: "=", with: ""))
      ])
    case 4:
      guard length <= UInt64(reader.remaining) else {
        throw IngressError.malformedArchive("DAG-CBOR array exceeds input")
      }
      return .array(try (0..<Int(length)).map { _ in try decode(&reader, depth: depth + 1) })
    case 5:
      guard length <= UInt64(reader.remaining / 2) else {
        throw IngressError.malformedArchive("DAG-CBOR map exceeds input")
      }
      var values: [String: JSONValue] = [:]
      for _ in 0..<Int(length) {
        guard case .string(let key) = try decode(&reader, depth: depth + 1), values[key] == nil
        else { throw IngressError.malformedArchive("Invalid or duplicate DAG-CBOR map key") }
        values[key] = try decode(&reader, depth: depth + 1)
      }
      return .object(values)
    case 6:
      guard length == 42, case .object(let object) = try decode(&reader, depth: depth + 1),
        let encoded = object["$bytes"]?.string
      else { throw IngressError.malformedArchive("Invalid DAG-CBOR link") }
      let padded = encoded + String(repeating: "=", count: (4 - encoded.count % 4) % 4)
      guard let data = Data(base64Encoded: padded), data.count > 1, data.first == 0 else {
        throw IngressError.malformedArchive("Invalid CID bytes")
      }
      let cid = base32(Array(data.dropFirst()))
      guard ATProtoRecordJSON.validCID(cid) else {
        throw IngressError.malformedArchive("Malformed DAG-CBOR CID")
      }
      return .object(["$link": .string(cid)])
    default: throw IngressError.malformedArchive("Unsupported DAG-CBOR major type")
    }
  }
  private static func base32(_ data: [UInt8]) -> String {
    let alphabet = Array("abcdefghijklmnopqrstuvwxyz234567".utf8)
    var output: [UInt8] = [98]
    var buffer: UInt32 = 0
    var bits = 0
    for byte in data {
      buffer = (buffer << 8) | UInt32(byte)
      bits += 8
      while bits >= 5 {
        bits -= 5
        output.append(alphabet[Int((buffer >> bits) & 31)])
      }
    }
    if bits > 0 { output.append(alphabet[Int((buffer << (5 - bits)) & 31)]) }
    return String(decoding: output, as: UTF8.self)
  }
}
