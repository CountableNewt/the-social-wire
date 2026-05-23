import Foundation
import GatewayCore
import Testing
@testable import AppView

struct BootstrapStreamSelectionTests {
  @Test func firstUnreadPrefersPriorityOrder() {
    let my = SidebarPublicationRow(
      publicationId: "pub-my",
      subscriptionPublicationId: nil as String?,
      authorDid: "did:plc:a",
      authorHandle: nil as String?,
      title: "Mine",
      iconUrl: nil as String?,
      avatarUrl: nil as String?,
      discoveredAt: Date(),
      appViewScope: PublicationAppViewScope(
        authorDid: "did:plc:a",
        publicationAtUri: nil as String?,
        publicationScopeAtUris: [],
        publicationSiteUrls: []
      ),
      unreadCount: 0
    )
    let subscribed = SidebarPublicationRow(
      publicationId: "pub-sub",
      subscriptionPublicationId: nil as String?,
      authorDid: "did:plc:b",
      authorHandle: nil as String?,
      title: "Sub",
      iconUrl: nil as String?,
      avatarUrl: nil as String?,
      discoveredAt: Date(),
      appViewScope: PublicationAppViewScope(
        authorDid: "did:plc:b",
        publicationAtUri: nil as String?,
        publicationScopeAtUris: [],
        publicationSiteUrls: []
      ),
      unreadCount: 2
    )

    let selected = BootstrapStreamSelection.firstUnreadPublicationId(
      myPublications: [my],
      subscribedUnfoldered: [subscribed],
      following: [],
      unreadCounts: ["pub-sub": 2]
    )

    #expect(selected == "pub-sub")
  }
}
