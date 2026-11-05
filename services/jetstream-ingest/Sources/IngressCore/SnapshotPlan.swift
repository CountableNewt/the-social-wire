import Foundation

public struct SnapshotPlan: Decodable, Sendable {
  public struct Segment: Decodable, Sendable {
    public struct BlockRange: Decodable, Sendable {
      public let first: Int
      public let last: Int
    }
    public let name: String
    public let index: Int
    public let checksum: String
    public let minSeq: Int64
    public let maxSeq: Int64
    public let mode: String
    public let blocks: [BlockRange]?
  }
  public let plannedThroughSeq: Int64
  public let sealedTipSeq: Int64
  public let segments: [Segment]

  public func validate(after: Int64, sealed: Int64?) throws {
    guard plannedThroughSeq >= after || segments.isEmpty && sealedTipSeq < after,
      plannedThroughSeq <= sealedTipSeq, sealed == nil || sealed == sealedTipSeq
    else { throw IngressError.sourceDiscontinuity }
    var previous: Int64 = -1
    for segment in segments {
      guard segment.minSeq >= 0, segment.maxSeq >= segment.minSeq, segment.maxSeq > after,
        min(segment.maxSeq, sealedTipSeq) <= plannedThroughSeq, segment.minSeq >= previous,
        segment.index >= 0, !segment.name.isEmpty,
        segment.checksum.utf8.count == 16,
        segment.checksum.utf8.allSatisfy({
          (48...57).contains($0) || (65...70).contains($0) || (97...102).contains($0)
        }), ["segment", "blocks"].contains(segment.mode)
      else { throw IngressError.malformedArchive("Invalid snapshot segment") }
      previous = segment.maxSeq
      if segment.mode == "blocks" {
        guard let blocks = segment.blocks, !blocks.isEmpty else {
          throw IngressError.malformedArchive("Missing snapshot block ranges")
        }
        var last = -1
        for range in blocks {
          guard range.first >= 0, range.last >= range.first, range.first > last,
            range.last <= 1_000_000
          else { throw IngressError.malformedArchive("Invalid snapshot block range") }
          last = range.last
        }
      }
    }
    if plannedThroughSeq < sealedTipSeq && plannedThroughSeq <= after {
      throw IngressError.malformedArchive("Snapshot plan made no progress")
    }
  }
}
