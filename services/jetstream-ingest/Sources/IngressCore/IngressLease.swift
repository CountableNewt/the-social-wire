public struct IngressLease: Sendable {
  public let name: String
  public let owner: String
  public let token: Int64
  public init(name: String, owner: String, token: Int64) {
    self.name = name
    self.owner = owner
    self.token = token
  }
}
