import Foundation

/// Validate reserved ATProto JSON sentinels before a live record can advance the cursor.
enum ATProtoRecordJSON {
  static func normalize(_ value: JSONValue, depth: Int = 0) throws -> JSONValue {
    guard depth <= 128 else {
      throw IngressError.invalidEvent("Record nesting exceeds safety limit")
    }
    switch value {
    case .object(let body):
      if let encoded = body["$bytes"] {
        guard body.count == 1, let text = encoded.string else {
          throw IngressError.invalidEvent("Invalid ATProto bytes sentinel")
        }
        let padded = text + String(repeating: "=", count: (4 - text.count % 4) % 4)
        guard let bytes = Data(base64Encoded: padded) else {
          throw IngressError.invalidEvent("Invalid ATProto bytes encoding")
        }
        return .object([
          "$bytes": .string(bytes.base64EncodedString().replacingOccurrences(of: "=", with: ""))
        ])
      }
      if let link = body["$link"] {
        guard body.count == 1, let text = link.string, validCID(text) else {
          throw IngressError.invalidEvent("Invalid ATProto CID sentinel")
        }
        return value
      }
      return .object(try body.mapValues { try normalize($0, depth: depth + 1) })
    case .array(let array): return .array(try array.map { try normalize($0, depth: depth + 1) })
    case .double(let number):
      guard number.isFinite else {
        throw IngressError.invalidEvent("Nonfinite ATProto JSON number")
      }
      return value
    default: return value
    }
  }

  static func validCID(_ text: String) -> Bool {
    guard text.utf8.count <= 256 else { return false }
    let bytes: [UInt8]
    if text.first == "b" {
      let alphabet = Array("abcdefghijklmnopqrstuvwxyz234567".utf8)
      var output: [UInt8] = []
      var buffer: UInt32 = 0
      var bits = 0
      for byte in text.utf8.dropFirst() {
        guard let index = alphabet.firstIndex(of: byte) else { return false }
        buffer = buffer << 5 | UInt32(index)
        bits += 5
        if bits >= 8 {
          bits -= 8
          output.append(UInt8(truncatingIfNeeded: buffer >> bits))
          buffer &= (1 << bits) - 1
        }
      }
      guard buffer == 0 else { return false }
      bytes = output
    } else if text.first == "z" || text.hasPrefix("Qm") {
      let source = text.first == "z" ? String(text.dropFirst()) : text
      let alphabet = Array("123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz".utf8)
      var output: [UInt8] = []
      for byte in source.utf8 {
        guard let digit = alphabet.firstIndex(of: byte) else { return false }
        var carry = digit
        for index in output.indices.reversed() {
          carry += Int(output[index]) * 58
          output[index] = UInt8(carry & 255)
          carry >>= 8
        }
        while carry > 0 {
          output.insert(UInt8(carry & 255), at: 0)
          carry >>= 8
        }
      }
      bytes = Array(repeating: 0, count: source.prefix(while: { $0 == "1" }).count) + output
      if text.hasPrefix("Qm") { return bytes.count == 34 && bytes.prefix(2) == [0x12, 0x20] }
    } else {
      return false
    }
    var offset = 0
    func varint() -> UInt64? {
      var output: UInt64 = 0
      for index in 0..<10 {
        guard offset < bytes.count else { return nil }
        let byte = bytes[offset]
        offset += 1
        guard index < 9 || byte <= 1 else { return nil }
        output |= UInt64(byte & 127) << (index * 7)
        if byte < 128 { return index > 0 && byte == 0 ? nil : output }
      }
      return nil
    }
    guard varint() == 1, let codec = varint(), codec > 0, varint() != nil, let length = varint(),
      length <= UInt64(bytes.count - offset)
    else { return false }
    return Int(length) == bytes.count - offset
  }
}
