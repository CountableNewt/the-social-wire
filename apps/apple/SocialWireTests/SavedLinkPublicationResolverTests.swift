import Foundation
import Testing
@testable import SocialWire

@Suite("SavedLinkPublicationResolver")
struct SavedLinkPublicationResolverTests {
    @Test("matches standard.site publication via publicationSiteUrls")
    func matchesStandardSitePublicationBySiteUrls() {
        let publication = DiscoveredPublication(
            publicationId: "at://did:plc:alice/site.standard.publication/main",
            subscriptionPublicationId: "at://did:plc:alice/site.standard.publication/main",
            authorDid: "did:plc:alice",
            authorHandle: "alice.bsky.social",
            title: "Alice's Newsletter",
            iconUrl: "https://newsletter.example.com/icon.png",
            avatarUrl: nil,
            publicationSiteUrls: ["https://newsletter.example.com"],
            discoveredAt: "2026-01-01T00:00:00.000Z"
        )
        let scopes = [
            publication.publicationId: PublicationAppViewScopeDTO(
                authorDid: publication.authorDid,
                publicationAtUri: publication.publicationId,
                publicationScopeAtUris: [publication.publicationId],
                publicationSiteUrls: ["https://newsletter.example.com"]
            ),
        ]
        let save = MergedLatrSave.external(
            MergedLatrExternalSave(
                normalizedUrl: "https://newsletter.example.com/posts/hello",
                url: "https://newsletter.example.com/posts/hello",
                savedAt: "2026-01-01T00:00:00.000Z",
                externalRkey: "ext",
                itemRkey: "item",
                externalUri: "at://did:plc:me/link.latr.saved.external/ext",
                itemUri: "at://did:plc:me/link.latr.saved.item/item",
                subjectUri: "at://did:plc:me/link.latr.saved.external/ext",
                state: "unread",
                linkedWebUrl: nil
            )
        )

        let chip = SavedLinkPublicationResolver.resolve(
            for: save,
            sidebarPublications: [publication],
            publicationScopes: scopes
        )

        #expect(chip?.name == "Alice's Newsletter")
        #expect(chip?.faviconURL?.absoluteString == "https://newsletter.example.com/icon.png")
    }

    @Test("falls back to sidebar title when metadata is missing")
    func fallsBackToSidebarTitleWithoutMetadata() {
        let publication = DiscoveredPublication(
            publicationId: "at://did:plc:alice/site.standard.publication/main",
            subscriptionPublicationId: "at://did:plc:alice/site.standard.publication/main",
            authorDid: "did:plc:alice",
            authorHandle: "alice.bsky.social",
            title: "Alice's Newsletter",
            iconUrl: nil,
            avatarUrl: nil,
            publicationSiteUrls: ["https://newsletter.example.com"],
            discoveredAt: "2026-01-01T00:00:00.000Z"
        )
        let save = MergedLatrSave.external(
            MergedLatrExternalSave(
                normalizedUrl: "https://newsletter.example.com/posts/hello",
                url: "https://newsletter.example.com/posts/hello",
                savedAt: "2026-01-01T00:00:00.000Z",
                externalRkey: "ext",
                itemRkey: "item",
                externalUri: "at://did:plc:me/link.latr.saved.external/ext",
                itemUri: "at://did:plc:me/link.latr.saved.item/item",
                subjectUri: "at://did:plc:me/link.latr.saved.external/ext",
                state: "unread",
                linkedWebUrl: nil
            )
        )

        let chip = SavedLinkPublicationResolver.resolve(
            for: save,
            sidebarPublications: [publication],
            publicationScopes: [:]
        )

        #expect(chip?.name == "Alice's Newsletter")
        #expect(chip?.faviconURL?.absoluteString == "https://newsletter.example.com/favicon.ico")
    }
}
