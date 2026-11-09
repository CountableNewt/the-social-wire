import Crypto
import Foundation
import P256K

struct RepositorySigningKey: Sendable, Equatable {
  let codec: Int
  let compressed: Data

  init(multibase: String) throws {
    guard multibase.first == "z", multibase.count <= 128 else {
      throw RepositorySnapshotError.invalid("invalid signing multibase")
    }
    let alphabet = Array("123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz".utf8)
    var bytes: [UInt8] = []
    let input = Array(multibase.utf8.dropFirst())
    for character in input {
      guard let digit = alphabet.firstIndex(of: character) else {
        throw RepositorySnapshotError.invalid("invalid signing key")
      }
      var carry = digit
      for index in bytes.indices {
        carry += Int(bytes[index]) * 58
        bytes[index] = UInt8(carry & 255)
        carry >>= 8
      }
      while carry > 0 {
        bytes.append(UInt8(carry & 255))
        carry >>= 8
      }
    }
    let leadingZeros = input.prefix { $0 == 49 }.count
    var reader = RepositoryBinaryCursor(
      Data(repeating: 0, count: leadingZeros) + Data(bytes.reversed()))
    codec = try reader.varint()
    compressed = try reader.read(reader.remaining)
    guard [0xe7, 0x1200].contains(codec), compressed.count == 33 else {
      throw RepositorySnapshotError.invalid("unsupported signing curve")
    }
    if codec == 0xe7 {
      _ = try P256K.Signing.PublicKey(dataRepresentation: compressed, format: .compressed)
    } else {
      _ = try P256.Signing.PublicKey(compressedRepresentation: compressed)
    }
  }

  func verify(signature: Data, message: Data) throws {
    guard signature.count == 64 else {
      throw RepositorySnapshotError.invalid("invalid compact signature")
    }
    let valid: Bool
    if codec == 0xe7 {
      let key = try P256K.Signing.PublicKey(dataRepresentation: compressed, format: .compressed)
      let sig = try P256K.Signing.ECDSASignature(compactRepresentation: signature)
      valid = key.isValidSignature(sig, for: message)
    } else {
      // Crypto's P256 verifier accepts either S normalization. ATProto requires
      // low-S, so check the fixed-width integer before verifying the digest.
      let halfOrder = Data([
        0x7f, 0xff, 0xff, 0xff, 0x80, 0x00, 0x00, 0x00, 0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
        0xff,
        0xde, 0x73, 0x7d, 0x56, 0xd3, 0x8b, 0xcf, 0x42, 0x79, 0xdc, 0xe5, 0x61, 0x7e, 0x31, 0x92,
        0xa8,
      ])
      let s = Data(signature.suffix(32))
      guard !halfOrder.lexicographicallyPrecedes(s) else {
        throw RepositorySnapshotError.invalid("high-S signature")
      }
      let key = try P256.Signing.PublicKey(compressedRepresentation: compressed)
      valid = key.isValidSignature(
        try P256.Signing.ECDSASignature(rawRepresentation: signature), for: message)
    }
    guard valid else { throw RepositorySnapshotError.invalid("invalid commit signature") }
  }
}
