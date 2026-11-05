import Foundation
import Testing

@testable import IngressCore

@Test func upstreamSealedMetadataChecksumAndOffsetsMatch() throws {
  let url = Bundle.module.url(
    forResource: "golden_seal", withExtension: "bin", subdirectory: "Fixtures")!
  let data = try Data(contentsOf: url)
  var reader = BinaryCursor(data)
  reader.offset = 4
  let checksum = String(format: "%016llx", try reader.little(8))
  let segment = try ArchiveSegment(url: url, checksum: checksum)
  #expect(segment.minimum == 1)
  #expect(segment.maximum == 2)
  #expect(segment.blocks.count == 1)
  var corrupt = data
  corrupt[98] ^= 1
  let copy = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
  try corrupt.write(to: copy)
  defer { try? FileManager.default.removeItem(at: copy) }
  #expect(throws: (any Error).self) { try ArchiveSegment(url: copy, checksum: checksum) }
}

@Test func officialV2WriterFixtureDecodesRecordsAcrossBlockSeam() throws {
  let url = Bundle.module.url(
    forResource: "official-v2-segment", withExtension: "jss", subdirectory: "Fixtures")!
  let data = try Data(contentsOf: url)
  var reader = BinaryCursor(data)
  reader.offset = 4
  let checksum = String(format: "%016llx", try reader.little(8))
  let archive = try ArchiveSegment(url: url, checksum: checksum)
  let events = try archive.blocks.indices.flatMap { try archive.decodeBlock($0, from: url) }
  #expect(archive.blocks.count == 2)
  #expect(events.map(\.sequence) == [100, 101, 102])
  #expect(events.map(\.operation) == ["create", "update", "delete"])
  #expect(events[0].payload["commit"]?["record"] == .object(["hello": .integer(5)]))
  #expect(events[1].timeUS == 1_700_000_000_000_007)
  #expect(events[2].payload["commit"]?["record"] == nil)
}

@Test func officialDictionaryCompressedLiveFrameUsesSameValidatedEnvelope() throws {
  let dictionary = try Data(
    contentsOf: Bundle.module.url(
      forResource: "official-live", withExtension: "zdict", subdirectory: "Fixtures")!)
  let frame = try Data(
    contentsOf: Bundle.module.url(
      forResource: "official-live-frame", withExtension: "zstd", subdirectory: "Fixtures")!)
  let decoded = try ArchiveBlockDecoder.decompress(
    frame, maximumBytes: 32 << 20, dictionary: dictionary)
  let event = try #require(try IngressEvent.decodeLive(decoded))
  #expect(event.sequence == 42)
  #expect(event.timeUS == 1_793_840_523_123_456)
  #expect(throws: (any Error).self) { try ArchiveBlockDecoder.decompress(frame) }
  #expect(throws: (any Error).self) {
    try ArchiveBlockDecoder.decompress(frame, maximumBytes: 10, dictionary: dictionary)
  }
}
