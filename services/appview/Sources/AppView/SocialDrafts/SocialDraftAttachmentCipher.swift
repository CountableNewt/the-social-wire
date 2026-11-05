import Crypto
import Foundation

/// The persisted TSWDA v1 format shared with the existing Go draft store.
/// AAD binds every independently authenticated frame to owner, asset and order.
struct SocialDraftAttachmentCipher: Sendable {
  static let maximumFileBytes = 100 << 20
  private static let chunkBytes = 64 << 10
  private static let magic = Data([0x54, 0x53, 0x57, 0x44, 0x41, 0x01])
  private let key: SymmetricKey

  init(key: Data) throws {
    guard key.count == 32 else { throw SocialDraftAttachmentError.invalidKey }
    self.key = SymmetricKey(data: key)
  }

  func sealMetadata(_ data: Data, owner: String, assetID: String) throws -> Data {
    let aad = try JSONSerialization.data(withJSONObject: ["socialdraft", 1, owner, assetID, "metadata", 0],
      options: [.withoutEscapingSlashes])
    guard let value = try AES.GCM.seal(data, using: key, authenticating: aad).combined else {
      throw SocialDraftAttachmentError.invalidAttachment
    }
    return value
  }

  func openMetadata(_ data: Data, owner: String, assetID: String) throws -> Data {
    let aad = try JSONSerialization.data(withJSONObject: ["socialdraft", 1, owner, assetID, "metadata", 0],
      options: [.withoutEscapingSlashes])
    return try AES.GCM.open(AES.GCM.SealedBox(combined: data), using: key, authenticating: aad)
  }

  /// Reads and writes bounded frames; callers own and clean up both file handles.
  func encrypt(input: FileHandle, output: FileHandle, owner: String, assetID: String, expectedBytes: Int) throws {
    try Self.validateSize(expectedBytes)
    try output.write(contentsOf: Self.magic)
    var total = 0
    var index: UInt64 = 0
    while let plain = try Self.read(input, maximum: Self.chunkBytes) {
      total += plain.count
      guard total <= expectedBytes else { throw SocialDraftAttachmentError.invalidAttachment }
      try writeFrame(plain, to: output, owner: owner, assetID: assetID, index: index)
      index += 1
    }
    guard total == expectedBytes else { throw SocialDraftAttachmentError.invalidAttachment }
    // An authenticated empty frame is mandatory, including for an empty input.
    try writeFrame(Data(), to: output, owner: owner, assetID: assetID, index: index)
  }

  /// Decrypt into a private temporary file. Do not expose its contents until
  /// this returns: the mandatory terminal frame authenticates completeness.
  func decrypt(input: FileHandle, output: FileHandle, owner: String, assetID: String, expectedBytes: Int) throws {
    try Self.validateSize(expectedBytes)
    guard try Self.read(input, maximum: Self.magic.count) == Self.magic else {
      throw SocialDraftAttachmentError.invalidAttachment
    }
    var total = 0
    var index: UInt64 = 0
    while true {
      guard let size = try Self.read(input, maximum: 4), size.count == 4 else {
        throw SocialDraftAttachmentError.invalidAttachment
      }
      let length = size.reduce(UInt32(0)) { ($0 << 8) | UInt32($1) }
      guard length >= 28, length <= Self.chunkBytes + 28,
        let frame = try Self.read(input, maximum: Int(length)), frame.count == length else {
        throw SocialDraftAttachmentError.invalidAttachment
      }
      let plain: Data
      do {
        plain = try AES.GCM.open(AES.GCM.SealedBox(combined: frame), using: key,
          authenticating: additionalData(owner: owner, assetID: assetID, index: index))
      } catch {
        throw SocialDraftAttachmentError.invalidAttachment
      }
      if plain.isEmpty {
        guard total == expectedBytes, try Self.read(input, maximum: 1) == nil else {
          throw SocialDraftAttachmentError.invalidAttachment
        }
        return
      }
      total += plain.count
      guard total <= expectedBytes else { throw SocialDraftAttachmentError.invalidAttachment }
      try output.write(contentsOf: plain)
      index += 1
    }
  }

  private func writeFrame(_ plain: Data, to output: FileHandle, owner: String, assetID: String, index: UInt64) throws {
    let sealed = try AES.GCM.seal(plain, using: key,
      authenticating: additionalData(owner: owner, assetID: assetID, index: index))
    guard let frame = sealed.combined else { throw SocialDraftAttachmentError.invalidAttachment }
    let length = UInt32(frame.count)
    let size = Data([24, 16, 8, 0].map { UInt8(truncatingIfNeeded: length >> $0) })
    try output.write(contentsOf: size)
    try output.write(contentsOf: frame)
  }

  private func additionalData(owner: String, assetID: String, index: UInt64) throws -> Data {
    try JSONSerialization.data(withJSONObject: ["socialdraft", 1, owner, assetID, "file", index],
      options: [.withoutEscapingSlashes])
  }

  private static func validateSize(_ bytes: Int) throws {
    guard bytes >= 0, bytes <= maximumFileBytes else { throw SocialDraftAttachmentError.sizeLimit }
  }

  private static func read(_ input: FileHandle, maximum: Int) throws -> Data? {
    var result = Data()
    while result.count < maximum {
      guard let next = try input.read(upToCount: maximum - result.count), !next.isEmpty else { break }
      result.append(next)
    }
    return result.isEmpty ? nil : result
  }
}
