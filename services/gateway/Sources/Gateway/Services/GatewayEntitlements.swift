import GatewayCore

struct GatewayEntitlements: Sendable {
  let gatingEnabled: Bool
  let proDIDs: Set<String>

  func hasPro(did: String) -> Bool {
    !did.isEmpty && did != GatewayInternalTrustAuthMiddleware.anonymousDiscoveryDID && proDIDs.contains(did)
  }
}
