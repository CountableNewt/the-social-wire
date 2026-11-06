import Foundation
import Testing
@testable import AppView

@Suite struct SocialDraftAttachmentCipherTests {
  private let key = Data(0..<32)
  private let owner = "did:plc:owner"
  private let assetID = "asset-fixture"
  private let fixture = "5453574441010000003700000000000000000000000146a1d69a30d451706a0e1656ccd6489e63fe683144d626e51574708ed8e75af30a251e4d1b1fcadbb5ab9c0000001c000000000000000000000002a5a92b9787d259134be0042a13166b24"

  @Test func decryptsExistingGoFrames() throws {
    let clear = Data("Swift and Go private drafts".utf8)
    #expect(try transform(hex(fixture), decrypt: true, expected: clear.count) == clear)
  }

  @Test(arguments: [0, 1, 65536, 131091]) func roundTripsBoundedFrames(size: Int) throws {
    let plain = Data((0..<size).map { UInt8(truncatingIfNeeded: $0) })
    let encrypted = try transform(plain, expected: size)
    #expect(try transform(encrypted, decrypt: true, expected: size) == plain)
  }

  @Test func requiresTerminalAuthenticationAndRejectsTrailingData() throws {
    let valid = hex(fixture)
    for malformed in [Data(valid.dropLast(32)), valid + Data([0]), Data(valid.dropLast())] {
      #expect(throws: SocialDraftAttachmentError.invalidAttachment) {
        try transform(malformed, decrypt: true, expected: 27)
      }
    }
  }

  @Test func bindsOwnerAssetOrderAndLength() throws {
    let valid = hex(fixture)
    for (owner, asset, expected) in [("did:plc:other", assetID, 27), (self.owner, "other", 27), (self.owner, assetID, 26)] {
      #expect(throws: SocialDraftAttachmentError.invalidAttachment) {
        try transform(valid, decrypt: true, expected: expected, owner: owner, asset: asset)
      }
    }
    var corrupt = valid
    corrupt[25] ^= 1
    #expect(throws: SocialDraftAttachmentError.invalidAttachment) {
      try transform(corrupt, decrypt: true, expected: 27)
    }
  }

  @Test func rejectsInvalidKeysAndUnboundedSizes() throws {
    #expect(throws: SocialDraftAttachmentError.invalidKey) { try SocialDraftAttachmentCipher(key: Data(repeating: 0, count: 16)) }
    #expect(throws: SocialDraftAttachmentError.sizeLimit) { try transform(Data(), expected: SocialDraftAttachmentCipher.maximumFileBytes + 1) }
    #expect(throws: SocialDraftAttachmentError.invalidAttachment) { try transform(Data([1]), expected: 2) }
  }

  @Test func rejectsReorderedFramesAndMetadataSubstitution() throws {
    let plain = Data(repeating: 7, count: 131072)
    let encrypted = try transform(plain, expected: plain.count)
    func frameEnd(_ offset: Int) -> Int {
      let length = encrypted[offset..<offset + 4].reduce(0) { ($0 << 8) | Int($1) }
      return offset + 4 + length
    }
    let firstEnd = frameEnd(6)
    let secondEnd = frameEnd(firstEnd)
    let reordered = Data(encrypted.prefix(6)) + Data(encrypted[firstEnd..<secondEnd])
      + Data(encrypted[6..<firstEnd]) + Data(encrypted[secondEnd...])
    #expect(throws: SocialDraftAttachmentError.invalidAttachment) { try transform(reordered, decrypt: true, expected: plain.count) }
    let cipher = try SocialDraftAttachmentCipher(key: key)
    let metadata = try cipher.sealMetadata(Data("private name".utf8), owner: owner, assetID: assetID)
    #expect(try cipher.openMetadata(metadata, owner: owner, assetID: assetID) == Data("private name".utf8))
    #expect(throws: (any Error).self) { try cipher.openMetadata(metadata, owner: "did:plc:other", assetID: assetID) }
  }

  private func transform(_ data: Data, decrypt: Bool = false, expected: Int, owner: String? = nil, asset: String? = nil) throws -> Data {
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let source = directory.appendingPathComponent("source")
    let target = directory.appendingPathComponent("target")
    try data.write(to: source)
    try Data().write(to: target)
    let input = try FileHandle(forReadingFrom: source)
    let output = try FileHandle(forWritingTo: target)
    defer { try? input.close(); try? output.close() }
    let cipher = try SocialDraftAttachmentCipher(key: key)
    if decrypt { try cipher.decrypt(input: input, output: output, owner: owner ?? self.owner, assetID: asset ?? assetID, expectedBytes: expected) }
    else { try cipher.encrypt(input: input, output: output, owner: owner ?? self.owner, assetID: asset ?? assetID, expectedBytes: expected) }
    return try Data(contentsOf: target)
  }

  private func hex(_ value: String) -> Data {
    Data(stride(from: 0, to: value.count, by: 2).map { offset in
      let start = value.index(value.startIndex, offsetBy: offset)
      return UInt8(value[start..<value.index(start, offsetBy: 2)], radix: 16)!
    })
  }
}
