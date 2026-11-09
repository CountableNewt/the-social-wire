import Crypto
import Foundation

struct RepositoryCID: Hashable, Sendable {
  let bytes: Data
  var isDAGCBOR: Bool { bytes[1] == 0x71 }

  init(bytes: Data) throws {
    let encoded = Array(bytes)
    guard encoded.count == 36, encoded[0] == 1, [0x55, 0x71].contains(encoded[1]),
      encoded[2] == 0x12, encoded[3] == 0x20
    else {
      throw RepositorySnapshotError.invalid("invalid ATProto CID")
    }
    self.bytes = Data(encoded)
  }

  init(_ text: String) throws {
    guard text.first == "b", text.utf8.count == 59 else {
      throw RepositorySnapshotError.invalid("invalid CID")
    }
    let alphabet = Array("abcdefghijklmnopqrstuvwxyz234567".utf8)
    var output = Data()
    var buffer: UInt32 = 0
    var bits = 0
    for byte in text.utf8.dropFirst() {
      guard let value = alphabet.firstIndex(of: byte) else {
        throw RepositorySnapshotError.invalid("invalid CID alphabet")
      }
      buffer = buffer << 5 | UInt32(value)
      bits += 5
      if bits >= 8 {
        bits -= 8
        output.append(UInt8(truncatingIfNeeded: buffer >> bits))
        buffer &= (1 << bits) - 1
      }
    }
    guard buffer == 0 else { throw RepositorySnapshotError.invalid("noncanonical CID") }
    try self.init(bytes: output)
  }

  static func hash(_ data: Data) -> Self {
    // The fixed multicodec and digest length produce a valid CID by construction.
    try! Self(bytes: Data([1, 0x71, 0x12, 0x20]) + Data(SHA256.hash(data: data)))
  }

  var string: String {
    let alphabet = Array("abcdefghijklmnopqrstuvwxyz234567".utf8)
    var output: [UInt8] = [98]
    var buffer: UInt32 = 0
    var bits = 0
    for byte in bytes {
      buffer = buffer << 8 | UInt32(byte)
      bits += 8
      while bits >= 5 {
        bits -= 5
        output.append(alphabet[Int(buffer >> bits & 31)])
      }
    }
    if bits > 0 { output.append(alphabet[Int(buffer << (5 - bits) & 31)]) }
    return String(decoding: output, as: UTF8.self)
  }

  func verify(_ data: Data) throws {
    guard Data(SHA256.hash(data: data)) == bytes.suffix(32) else {
      throw RepositorySnapshotError.invalid("block hash mismatch")
    }
  }
}
