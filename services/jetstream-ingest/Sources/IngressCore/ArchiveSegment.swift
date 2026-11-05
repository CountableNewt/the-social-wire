import ArchiveChecksums
import Foundation

public struct ArchiveSegment: Sendable {
  public struct Block: Sendable {
    public let offset: Int
    public let compressedSize: Int
  }
  public let blocks: [Block]
  public let minimum: Int64
  public let maximum: Int64

  public init(url: URL, checksum: String) throws {
    let file = try FileHandle(forReadingFrom: url)
    defer { try? file.close() }
    let size = try file.seekToEnd()
    try file.seek(toOffset: 0)
    let header = try file.read(upToCount: 256) ?? Data()
    guard header.count == 256, String(decoding: header.prefix(4), as: UTF8.self) == "jss0" else {
      throw IngressError.malformedArchive("Invalid sealed segment header")
    }
    var reader = BinaryCursor(header)
    reader.offset = 4
    let storedHash = try reader.little(8)
    guard storedHash != 0, String(format: "%016llx", storedHash) == checksum.lowercased(),
      try reader.little(2) == 1
    else { throw IngressError.malformedArchive("Segment generation or version mismatch") }
    let count = Int(try reader.little(4))
    _ = try reader.read(8)
    let min = try reader.little(8)
    let max = try reader.little(8)
    guard min <= max, max <= UInt64(Int64.max) else {
      throw IngressError.malformedArchive("Invalid segment sequence bounds")
    }
    minimum = Int64(min)
    maximum = Int64(max)
    reader.offset = 58
    let footerOffset = try reader.little(8)
    reader.offset = 90
    let indexOffset = try reader.little(8)
    guard footerOffset == indexOffset, footerOffset >= 256, footerOffset <= size,
      size - footerOffset <= 128 << 20, UInt64(count) * 52 <= size - footerOffset
    else { throw IngressError.malformedArchive("Invalid segment footer bounds") }
    try file.seek(toOffset: footerOffset)
    let footer = try file.readToEnd() ?? Data()
    var metadata = Data(header.dropFirst(12))
    metadata.append(footer)
    let actual = metadata.withUnsafeBytes { ingress_xxh3($0.baseAddress, $0.count) }
    guard actual == storedHash else {
      throw IngressError.malformedArchive("Segment metadata checksum mismatch")
    }
    var index = BinaryCursor(footer)
    var output: [Block] = []
    var previousEnd = 256
    for _ in 0..<count {
      let offset = try index.little(8)
      let length = try index.little(4)
      _ = try index.read(40)
      guard offset >= UInt64(previousEnd), offset <= footerOffset,
        length <= UInt64(ArchiveBlockDecoder.maxBlockBytes), offset + 8 + length <= footerOffset
      else { throw IngressError.malformedArchive("Invalid segment block bounds") }
      output.append(Block(offset: Int(offset), compressedSize: Int(length)))
      previousEnd = Int(offset + 8 + length)
    }
    blocks = output
  }

  public func decodeBlock(_ index: Int, from url: URL) throws -> [IngressEvent] {
    let file = try FileHandle(forReadingFrom: url)
    defer { try? file.close() }
    let block = blocks[index]
    try file.seek(toOffset: UInt64(block.offset))
    var prefix = BinaryCursor(try file.read(upToCount: 8) ?? Data())
    guard try prefix.little(8) == UInt64(block.compressedSize) else {
      throw IngressError.malformedArchive("Segment frame length mismatch")
    }
    let frame = try file.read(upToCount: block.compressedSize) ?? Data()
    guard frame.count == block.compressedSize else {
      throw IngressError.malformedArchive("Truncated segment frame")
    }
    return try ArchiveBlockDecoder.decodeCompressed(frame)
  }
}
