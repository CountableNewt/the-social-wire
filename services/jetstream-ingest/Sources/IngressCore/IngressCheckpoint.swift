import Foundation

public struct IngressCheckpoint: Sendable {
  public let sequence: Int64?
  public let replayBytes: Int64
  public let state: String
  public let retries: Int
  public let resumes: Int
  public let etag: String?
  public let lastProgressAt: Date?
  public init(
    sequence: Int64?, replayBytes: Int64, state: String, retries: Int = 0, resumes: Int = 0,
    etag: String? = nil, lastProgressAt: Date? = nil
  ) {
    self.sequence = sequence
    self.replayBytes = replayBytes
    self.state = state
    self.retries = retries
    self.resumes = resumes
    self.etag = etag
    self.lastProgressAt = lastProgressAt
  }
  public var replayAfter: Int64 { max(0, (sequence ?? 0) - 1) }
}
