public struct ReplayProgress: Sendable {
  public var state: String
  public var after: Int64
  public var sealed: Int64
  public var bytes: Int64
  public var retries: Int
  public var resumes: Int
  public var etag: String?
  public init(
    state: String, after: Int64, sealed: Int64, bytes: Int64, retries: Int = 0, resumes: Int = 0,
    etag: String? = nil
  ) {
    self.state = state
    self.after = after
    self.sealed = sealed
    self.bytes = bytes
    self.retries = retries
    self.resumes = resumes
    self.etag = etag
  }
}
