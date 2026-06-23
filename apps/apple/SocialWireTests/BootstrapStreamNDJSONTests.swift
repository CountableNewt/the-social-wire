import Foundation
import Testing
@testable import SocialWire

struct BootstrapStreamNDJSONTests {
    @Test func parsesMultipleLines() {
        let json = """
        {"kind":"selectedPublication","selectedPublication":{"publicationId":"pub-a"}}
        {"kind":"done","done":{"refreshedAt":"2026-01-01T00:00:00.000Z"}}
        """
        let events = BootstrapStreamNDJSON.parseLines(json)
        #expect(events.count == 2)
        #expect(events[0].kind == .selectedPublication)
        #expect(events[1].kind == .done)
    }

    @Test func firstUnreadUsesPriorityOrder() {
        let subscribed = SidebarPublicationRowDTO(
            publicationId: "pub-sub",
            subscriptionPublicationId: nil,
            authorDid: "did:plc:b",
            authorHandle: nil,
            title: "Sub",
            iconUrl: nil,
            avatarUrl: nil,
            discoveredAt: "2026-01-01T00:00:00.000Z",
            appViewScope: PublicationAppViewScopeDTO(
                authorDid: "did:plc:b",
                publicationAtUri: nil,
                publicationScopeAtUris: [],
                publicationSiteUrls: []
            ),
            unreadCount: 2
        )

        let selected = BootstrapStreamSelection.firstUnreadPublicationId(
            myPublications: [],
            subscribedUnfoldered: [subscribed],
            following: [],
            unreadCounts: ["pub-sub": 2]
        )
        #expect(selected == "pub-sub")
    }
}
