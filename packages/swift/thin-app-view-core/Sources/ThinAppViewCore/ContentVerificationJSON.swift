import Foundation

/// Rejects duplicate object keys before Foundation can collapse ambiguous authority/error fields.
struct ContentVerificationJSON {
  private let bytes: [UInt8]
  private var offset = 0
  private var remaining = 100_000

  static func object(_ data: Data, maximumBytes: Int) throws -> [String: Any] {
    guard data.count <= maximumBytes else { throw AuthoritativeContentVerificationError.invalidResponse }
    var parser = Self(bytes: Array(data))
    try parser.value(depth: 0)
    parser.whitespace()
    guard parser.offset == parser.bytes.count,
      let object = try JSONSerialization.jsonObject(with: data) as? [String: Any]
    else { throw AuthoritativeContentVerificationError.invalidResponse }
    return object
  }

  private mutating func whitespace() {
    while offset < bytes.count, [9, 10, 13, 32].contains(bytes[offset]) { offset += 1 }
  }

  private mutating func consume(_ byte: UInt8) throws {
    whitespace()
    guard offset < bytes.count, bytes[offset] == byte else { throw AuthoritativeContentVerificationError.invalidResponse }
    offset += 1
  }

  private mutating func string() throws -> String {
    whitespace()
    let start = offset
    try consume(34)
    while offset < bytes.count {
      let byte = bytes[offset]
      offset += 1
      if byte == 34 {
        return try JSONDecoder().decode(String.self, from: Data(bytes[start..<offset]))
      }
      if byte == 92 { offset += 1 }
    }
    throw AuthoritativeContentVerificationError.invalidResponse
  }

  private mutating func value(depth: Int) throws {
    remaining -= 1
    whitespace()
    guard depth <= 64, remaining >= 0, offset < bytes.count else { throw AuthoritativeContentVerificationError.invalidResponse }
    switch bytes[offset] {
    case 123:
      offset += 1
      whitespace()
      if offset < bytes.count, bytes[offset] == 125 { offset += 1; return }
      var keys: Set<String> = []
      while true {
        let key = try string()
        guard keys.insert(key).inserted else { throw AuthoritativeContentVerificationError.invalidResponse }
        try consume(58)
        try value(depth: depth + 1)
        whitespace()
        guard offset < bytes.count else { throw AuthoritativeContentVerificationError.invalidResponse }
        if bytes[offset] == 125 { offset += 1; return }
        try consume(44)
      }
    case 91:
      offset += 1
      whitespace()
      if offset < bytes.count, bytes[offset] == 93 { offset += 1; return }
      while true {
        try value(depth: depth + 1)
        whitespace()
        guard offset < bytes.count else { throw AuthoritativeContentVerificationError.invalidResponse }
        if bytes[offset] == 93 { offset += 1; return }
        try consume(44)
      }
    case 34: _ = try string()
    default:
      let start = offset
      while offset < bytes.count, ![9, 10, 13, 32, 44, 93, 125].contains(bytes[offset]) { offset += 1 }
      guard offset > start else { throw AuthoritativeContentVerificationError.invalidResponse }
    }
  }
}
