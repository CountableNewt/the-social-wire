#if DEBUG
import SwiftUI

/// Deterministic shell used only by UI automation so navigation chrome can be tested without live OAuth.
struct NewsShellUITestHarness: View {
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    @State private var fixtureModel = SocialWireAppModel(gatewayURLSession: NewsShellUITestTransport.session())
    @State private var isShellConfigured = false
    @State private var lastFeedAction = "Ready"
    @State private var readAgeRevision = 0

    var body: some View {
        if ProcessInfo.processInfo.arguments.contains("--ui-testing-read-age") {
            readAgeFixture
        } else {
            shell
        }
    }

    private var readAgeFixture: some View {
        VStack(spacing: 24) {
            Text(lastFeedAction).accessibilityIdentifier("feed-action-result")
            FeedMarkReadButton(
                contextID: "read-age-fixture",
                refreshRevision: readAgeRevision,
                scopeTitle: "Test Feed",
                loadOptions: { onOptions in
                    let options = [
                        FeedReadAgeOption(days: 1, before: "2026-09-02T05:00:00Z", count: 6),
                        FeedReadAgeOption(days: 2, before: "2026-09-01T05:00:00Z", count: 4),
                        FeedReadAgeOption(days: 4, before: "2026-08-30T05:00:00Z", count: 1),
                        FeedReadAgeOption(days: 7, before: "2026-08-27T05:00:00Z", count: 1),
                        FeedReadAgeOption(days: 8, before: "2026-08-26T05:00:00Z", count: 1),
                    ].filter { readAgeRevision == 0 || $0.days != 2 }
                    if ProcessInfo.processInfo.arguments.contains("--ui-testing-read-age-stream") {
                        onOptions(Array(options.prefix(1)))
                        try await Task.sleep(for: .seconds(5))
                    }
                    onOptions(options)
                    return options
                },
                markAllRead: { lastFeedAction = "All Read" },
                markOlderRead: { lastFeedAction = "Read Before \($0.before)" },
                markAllUnread: { lastFeedAction = "All Unread" }
            )
            Button("Read Yesterday's Stories") { readAgeRevision += 1 }
                .accessibilityIdentifier("fixture-read-stories")
        }
    }

    private var shell: some View {
        Group {
            if isShellConfigured {
                NewsShellView()
                    .environment(fixtureModel)
                    .environment(\.horizontalSizeClass,
                                 ProcessInfo.processInfo.arguments.contains("--ui-testing-compact-window")
                                    ? .compact : ProcessInfo.processInfo.arguments.contains("--ui-testing-regular-window")
                                        ? .regular : horizontalSizeClass)
                    .safeAreaInset(edge: .top) {
                        if ProcessInfo.processInfo.arguments.contains("--ui-testing-shell-routing") {
                            VStack {
                                HStack {
                                    Button("Hide Following") {
                                        fixtureModel.feedPreferences.visibleFeeds = [.readLater, .archive, .subscribed]
                                    }
                                    .accessibilityIdentifier("fixture-hide-following")
                                    Button("Select Publication") {
                                        // Same completed selection observed after Profile/sidebar selection;
                                        // no live OAuth, repository, or feed network dependency.
                                        let publication = Self.fixturePublication
                                        fixtureModel.selectedPublication = publication
                                        fixtureModel.feedSelection = .publication(publication.publicationId)
                                        fixtureModel.selectedSidebar = .publication(publication.publicationId)
                                        fixtureModel.entries = [Self.entry(index: 1)]
                                    }
                                    .accessibilityIdentifier("fixture-select-publication")
                                }
                                .buttonStyle(.bordered)
                                HStack {
                                    Button("Refresh Preferences") {
                                        fixtureModel.feedPreferences.feedsWithUnreadCounts = fixtureModel.feedPreferences.feedsWithUnreadCounts.isEmpty ? [.subscribed] : []
                                    }
                                    .accessibilityIdentifier("fixture-refresh-preferences")
                                    Text(fixtureScopeLabel)
                                        .accessibilityIdentifier("fixture-reader-scope")
                                }
                            }
                            .padding(.top, 50)
                        }
                    }
            } else {
                ProgressView()
                    .task {
                        fixtureModel.feedPreferences = ReaderFeedPreferences(
                            visibleFeeds: [.readLater, .archive, .subscribed, .following],
                            feedsWithUnreadCounts: [])

                        if ProcessInfo.processInfo.arguments.contains("--ui-testing-sidebar-publications") {
                            configureSidebarPublications()
                        }
                        if ProcessInfo.processInfo.arguments.contains("--ui-testing-topics-lists") {
                            fixtureModel.standardReaderLists.configureFixture(
                                lists: [StandardReaderList(
                                    uri: "at://did:plc:fixture/app.standard-reader.list/main",
                                    name: "Fixture List", description: "Stories From Fixture Publications",
                                    creatorDid: "did:plc:fixture", publications: [Self.fixturePublication.publicationId],
                                    publicationDetails: nil, users: [], owned: true, saved: true)] +
                                    (ProcessInfo.processInfo.arguments.contains("--ui-testing-list-swipes") ? [
                                        StandardReaderList(uri: "at://did:plc:fixture/app.standard-reader.list/second",
                                            name: "Second List", description: nil, creatorDid: "did:plc:fixture",
                                            publications: [], publicationDetails: nil, users: [], owned: true, saved: true)
                                    ] : []),
                                entries: [Self.entry(index: 1)])
                            if ProcessInfo.processInfo.arguments.contains("--ui-testing-empty-lists") {
                                fixtureModel.standardReaderLists.configureFixture(lists: [], entries: [])
                            }
                        }
                        // Start from the default Subscribed selection; avoid live fetch work.
                        fixtureModel.readerListSource = .subscribed
                        fixtureModel.feedSelection = .topLevel(.subscribed)
                        isShellConfigured = true
                    }
            }
        }
    }

    private var fixtureScopeLabel: String {
        switch fixtureModel.feedSelection {
        case .topLevel(let source): "All \(source.rawValue)"
        case .publication: "One Publication"
        case .folder: "One Folder"
        case .standardList: "One List"
        case .savedSource: "One Saved Source"
        }
    }

    private func configureSidebarPublications() {
        let rows = ["Subscribed Publication", "Following Publication"].enumerated().map { index, title in
            SidebarPublicationRowDTO(
                publicationId: "at://did:plc:fixture/site.standard.publication/\(index)",
                subscriptionPublicationId: nil, authorDid: "did:plc:fixture", authorHandle: "fixture.example",
                title: title, iconUrl: nil, avatarUrl: nil, discoveredAt: "2026-10-26T00:00:00Z",
                appViewScope: PublicationAppViewScopeDTO(
                    authorDid: "did:plc:fixture", publicationAtUri: nil,
                    publicationScopeAtUris: [], publicationSiteUrls: []), unreadCount: 0)
        }
        let projection = PublicationSidebarResponseDTO(
            viewerDid: "did:plc:fixture", folders: nil, publicationPrefs: nil, folderSections: nil,
            allPublicationRows: rows, myPublications: [], subscribedUnfoldered: [rows[0]],
            followingTabPublications: [rows[1]], enrollAuthorDids: [],
            refreshedAt: "2026-10-26T00:00:00Z", unreadCountsByPublicationId: nil)
        fixtureModel.applyBootstrapStreamEvent(BootstrapStreamEventDTO(
            kind: .sidebarPriority, sidebarPriority: projection, sidebarSection: nil, unreadCounts: nil,
            selectedPublication: nil, entriesPage: nil, sidebarFolders: nil, lists: nil,
            warning: nil, error: nil, done: nil))
        fixtureModel.sidebarSubscribedPublicationsSectionExpanded = true
        fixtureModel.sidebarFollowingPublicationsSectionExpanded = true
    }

    private static let fixturePublication = DiscoveredPublication(
        publicationId: "at://did:plc:fixture/site.standard.publication/main",
        authorDid: "did:plc:fixture", authorHandle: "fixture.example",
        title: "Fixture Publication", discoveredAt: "2026-09-01T00:00:00Z")

    private static let fixtureTitle = "A Long Headline About Communities Building a More Open and Connected Social Web"

    private static func entry(index: Int) -> EntryListItem {
        EntryListItem(
            entryId: "ui-story-\(index)",
            title: index == 1 ? "A Short Headline" : fixtureTitle,
            summary: "People are sharing thoughtful stories and practical ideas across their communities every day.",
            publishedAt: "2026-09-02T12:00:00Z",
            formattedPublishedAt: "Today",
            thumbnailUrl: nil,
            thumbnailFallbackUrl: nil,
            originalUrl: "https://community-publications.example/story-\(index)"
        )
    }
}
#endif
