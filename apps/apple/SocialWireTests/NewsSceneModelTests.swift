import Foundation
import Testing
@testable import SocialWire

@Suite("News shell routing")
@MainActor
struct NewsSceneModelTests {
    private func fixture(replies: [OAuthTestTransport.Reply], delay: Duration = .zero) async throws -> (SocialWireAppModel, OAuthTestTransport) {
        let store = OAuthTestSessionStore()
        let auth = ATProtoOAuthService(keychain: store)
        let session = AuthSession(did: "did:plc:test", pdsURL: URL(string: "https://pds.example.test")!,
            tokenEndpoint: URL(string: "https://issuer.example/token")!, accessToken: "access",
            refreshToken: "refresh", tokenType: "DPoP", scope: ATProtoOAuthService.scopes, expiresAt: .distantFuture)
        let record: [String: Any] = [
            "session": try JSONSerialization.jsonObject(with: JSONEncoder().encode(session)),
            "dpopPrivateKey": Data(repeating: 1, count: 32).base64EncodedString(),
            "clientID": ATProtoOAuthConfig.clientID,
        ]
        store.set(try JSONSerialization.data(withJSONObject: record).base64EncodedString(), for: "oauth.session.v2")
        await auth.restoreSession()
        let transport = OAuthTestTransport(replies, delay: delay)
        let model = SocialWireAppModel(authService: auth, gatewayTransport: { try await transport.send($0) })
        return (model, transport)
    }

    @Test("Selecting the current aggregate clears a stale publication")
    func aggregateSelectionClearsStalePublication() async throws {
        let (model, _) = try await fixture(replies: [])
        defer { model.authService.signOut() }
        for source in [ReaderListSource.subscribed, .following] {
            model.readerListSource = source
            model.feedSelection = .topLevel(source)
            model.selectedPublication = DiscoveredPublication(
                publicationId: "at://did:plc:author/site.standard.publication/test",
                authorDid: "did:plc:author", authorHandle: "author.test",
                title: "Publication", discoveredAt: "2026-11-04T00:00:00Z"
            )
            model.entries = [EntryListItem(entryId: "stale-publication-entry", title: "Stale Story", summary: nil,
                                           publishedAt: "2026-11-04T00:00:00Z", thumbnailUrl: nil, thumbnailFallbackUrl: nil)]
            model.selectReaderListSource(source)
            #expect(model.entries.isEmpty)
            #expect(model.feedSelection == .topLevel(source))
            #expect(model.selectedPublication == nil)
            #expect(model.selectedSidebar == nil)
        }
    }

    @Test("Tabs keep their stable order")
    func availableTabOrder() {
        #expect(NewsTab.available == [.library, .saved, .search])
    }

    @Test("Primary slots contain only reader feeds")
    func primarySlotsContainReaderFeeds() {
        let model = SocialWireAppModel()
        model.loadPrimaryTabPreferences()
        #expect(model.primaryTabFeeds.allSatisfy { $0 == .subscribed || $0 == .following })
    }

    @Test("Other feed settings preserve a hidden Social section")
    func unrelatedFeedSettingsPreserveSectionVisibility() async {
        let model = SocialWireAppModel()
        model.feedPreferences.showSocial = false

        await model.setFeedDisplayOption(.showFeedOnly, for: .subscribed)
        #expect(!model.feedPreferences.showSocial)
        await model.setFeedVisible(.following, visible: false)
        #expect(!model.feedPreferences.showSocial)
        await model.setFeedUnreadCountVisible(.subscribed, visible: true)
        #expect(!model.feedPreferences.showSocial)
    }

    @Test("Last tab is restored per viewer")
    func restoresSelectedTabPerViewer() {
        let suiteName = "NewsSceneModelTests.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suiteName)!
        defer { defaults.removePersistentDomain(forName: suiteName) }

        let first = NewsSceneModel(defaults: defaults)
        first.updateContext(
            viewerDID: "did:plc:alice",
            availableTabs: [.library, .saved, .search]
        )
        first.select(.saved, availableTabs: [.library, .saved, .search])

        let restored = NewsSceneModel(defaults: defaults)
        restored.updateContext(
            viewerDID: "did:plc:alice",
            availableTabs: [.library, .saved, .search]
        )
        #expect(restored.selectedTab == .saved)

        restored.updateContext(
            viewerDID: "did:plc:bob",
            availableTabs: [.library, .saved, .search]
        )
        #expect(restored.selectedTab == .library)
    }

    @Test("A temporarily unavailable preferred feed is restored when its catalog arrives")
    func restoresPreferredGatedTab() {
        let suiteName = "NewsSceneModelTests.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suiteName)!
        defer { defaults.removePersistentDomain(forName: suiteName) }
        defaults.set(
            NewsTab.search.rawValue,
            forKey: NewsSceneModel.storageKey(viewerDID: "did:plc:alice")
        )

        let model = NewsSceneModel(defaults: defaults)
        model.updateContext(
            viewerDID: "did:plc:alice",
            availableTabs: [.library, .saved]
        )
        #expect(model.selectedTab == .library)

        model.updateContext(
            viewerDID: "did:plc:alice",
            availableTabs: [.library, .saved, .search]
        )
        #expect(model.selectedTab == .search)
    }

    @Test("Each tab owns an independent route path")
    func independentPaths() {
        let model = NewsSceneModel()
        model.navigate(to: .entry(id: "search-story"), in: .search)
        model.navigate(to: .savedLink(id: "saved-story"), in: .saved)

        #expect(model.path(for: .search) == [.entry(id: "search-story")])
        #expect(model.path(for: .saved) == [.savedLink(id: "saved-story")])
        #expect(model.path(for: .library).isEmpty)
    }

    @Test("Active route follows the selected tab without destroying other tab paths")
    func activeRouteFollowsSelectedTab() {
        let model = NewsSceneModel()
        model.navigate(to: .entry(id: "search-story"), in: .search)
        model.navigate(to: .savedLink(id: "saved-story"), in: .saved)

        model.select(.search, availableTabs: NewsTab.allCases)
        #expect(model.activeRoute == .entry(id: "search-story"))

        model.select(.saved, availableTabs: NewsTab.allCases)
        #expect(model.activeRoute == .savedLink(id: "saved-story"))

        model.resetPath(for: .saved)
        #expect(model.activeRoute == nil)
        #expect(model.path(for: .search) == [.entry(id: "search-story")])
    }

    @Test("Switching Read Later and Archive returns to the list and persists the cleared route",
          arguments: [ReaderListSource.readLater, .archive])
    func savedSourceSwitchClearsUnavailableDetail(destination: ReaderListSource) {
        let suiteName = "NewsSavedSourceSwitchTests.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suiteName)!
        defer { defaults.removePersistentDomain(forName: suiteName) }
        let model = NewsSceneModel(defaults: defaults)
        model.updateContext(viewerDID: "did:plc:alice", availableTabs: NewsTab.allCases)
        model.navigate(to: .savedLink(id: "previous-source-story"), in: .saved)
        model.navigate(to: .entry(id: "search-story"), in: .search)

        model.prepareForReaderSourceChange(
            from: destination == .archive ? .readLater : .archive, to: destination
        )
        model.select(.saved, availableTabs: NewsTab.allCases)

        #expect(model.activeRoute == nil)
        #expect(model.path(for: .search) == [.entry(id: "search-story")])
        let restored = NewsSceneModel(defaults: defaults)
        restored.updateContext(viewerDID: "did:plc:alice", availableTabs: NewsTab.allCases)
        #expect(restored.path(for: .saved).isEmpty)
    }

    @Test("Switching Subscribed and Following clears the previous feed detail",
          arguments: [ReaderListSource.subscribed, .following])
    func librarySourceSwitchClearsUnavailableDetail(destination: ReaderListSource) {
        let model = NewsSceneModel()
        model.navigate(to: .publication(id: "previous-publication"), in: .library)
        model.navigate(to: .entry(id: "previous-source-story"), in: .library)
        model.navigate(to: .savedLink(id: "saved-story"), in: .saved)

        model.prepareForReaderSourceChange(
            from: destination == .following ? .subscribed : .following, to: destination
        )
        model.select(.library, availableTabs: NewsTab.allCases)

        #expect(model.activeRoute == nil)
        #expect(model.path(for: .saved) == [.savedLink(id: "saved-story")])
    }

    @Test("Re-selecting the current source preserves its open article")
    func unchangedReaderSourcePreservesDetail() {
        let model = NewsSceneModel()
        model.navigate(to: .entry(id: "current-story"), in: .library)
        model.prepareForReaderSourceChange(from: .subscribed, to: .subscribed)
        #expect(model.path(for: .library) == [.entry(id: "current-story")])
    }

    @Test("Hiding the active discovery feed selects a visible tab and keeps Settings open")
    func hiddenFeedPreservesSettings() {
        let suiteName = "NewsSceneModelTests.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suiteName)!
        defer { defaults.removePersistentDomain(forName: suiteName) }

        for tab in [NewsTab.search] {
            let model = NewsSceneModel(defaults: defaults)
            model.updateContext(viewerDID: "did:plc:alice", availableTabs: NewsTab.allCases)
            model.select(tab, availableTabs: NewsTab.allCases)
            model.navigate(to: .profile, in: tab)
            model.navigate(to: .settings, in: tab)

            model.updateContext(
                viewerDID: "did:plc:alice",
                availableTabs: [.library, .saved]
            )
            #expect(model.selectedTab == .library)
            #expect(model.path(for: .library).last == .settings)
            #expect(model.path(for: tab) == [.profile, .settings])
        }
    }

    @Test("Routes restore independently for each viewer and window")
    func restoresRoutesPerViewerAndWindow() {
        let suiteName = "NewsSceneModelRouteTests.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suiteName)!
        defer { defaults.removePersistentDomain(forName: suiteName) }

        let first = NewsSceneModel(defaults: defaults, windowID: "window-a")
        first.updateContext(
            viewerDID: "did:plc:alice",
            availableTabs: [.library, .saved, .search]
        )
        first.setPath(
            [.publication(id: "at://alice/site.standard.publication/main")],
            for: .library
        )

        let restored = NewsSceneModel(defaults: defaults, windowID: "window-a")
        restored.updateContext(
            viewerDID: "did:plc:alice",
            availableTabs: [.library, .saved, .search]
        )
        #expect(
            restored.path(for: .library)
                == [.publication(id: "at://alice/site.standard.publication/main")]
        )

        let otherWindow = NewsSceneModel(defaults: defaults, windowID: "window-b")
        otherWindow.updateContext(
            viewerDID: "did:plc:alice",
            availableTabs: [.library, .saved, .search]
        )
        #expect(otherWindow.path(for: .library).isEmpty)

        restored.updateContext(
            viewerDID: "did:plc:bob",
            availableTabs: [.library, .saved, .search]
        )
        #expect(restored.path(for: .library).isEmpty)
    }
}
