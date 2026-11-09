public protocol PublicContentDiscovering: Sendable {
  /// An opportunistic page, not an exhaustive inventory or proof of deletion.
  func discover(scope: PublicContentDiscoveryScope) async throws -> Int
}
