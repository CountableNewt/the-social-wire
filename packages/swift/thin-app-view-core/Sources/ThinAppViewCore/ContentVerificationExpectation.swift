/// A verifier can replace only the local version observed before its network check.
/// A nil CID means the row must still be absent.
public struct ContentVerificationExpectation: Sendable {
  public let cid: String?
  public init(cid: String?) { self.cid = cid }
}
