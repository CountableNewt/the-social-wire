import SwiftUI

/// Compact tabs and an expanded split view share the same section and feed selections.
struct NewsShellView: View {
    @Environment(SocialWireAppModel.self) private var appModel
    #if os(iOS)
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    #endif
    @SceneStorage("the-social-wire.news-window-id.v1") private var windowID = UUID().uuidString
    @State private var sceneModel = NewsSceneModel()
    @State private var selectedSlot = NewsTabSlot.wire
    @State private var selectedSection = NewsRootSection.feeds
    @State private var columnVisibility = NavigationSplitViewVisibility.all
    @State private var sectionSelections: [NewsRootSection: NewsTabSlot] = [
        .readLater: .readLater,
        .feeds: .wire,
        .topics: .finance,
        .lists: .standardList("")
    ]
    @State private var slotFeeds: [NewsPrimaryFeed] = []
    @State private var isTabConfigurationLoaded = false
    @State private var isProfilePresented = false
    private var availableTabs: [NewsTab] {
        NewsTab.available(
            wire: NewsPrimaryFeed.algorithmicFeedsEnabled && appModel.feedPreferences.showWire,
            circle: NewsPrimaryFeed.algorithmicFeedsEnabled && appModel.feedPreferences.showCircle,
            finance: NewsPrimaryFeed.algorithmicFeedsEnabled && appModel.feedPreferences.showFinance,
            sports: NewsPrimaryFeed.algorithmicFeedsEnabled && appModel.feedPreferences.showSports
        )
    }

    private var activeNewsTab: NewsTab {
        selectedSlot.savedListSource != nil
            ? .saved
            : activePrimaryFeed?.newsTab ?? sceneModel.selectedTab
    }

    private var activePrimaryFeed: NewsPrimaryFeed? {
        selectedSlot.primaryFeed
    }

    private var navigableFeeds: [NewsPrimaryFeed] {
        let available = appModel.visiblePrimaryTabFeedChoices
        return slotFeeds.filter(available.contains) + available.filter { !slotFeeds.contains($0) }
    }

    private var feedDestinations: [NewsPrimaryFeed] {
        [.wire, .circle, .subscribed, .following].filter(isVisibleInSettings)
    }

    private var topicDestinations: [NewsPrimaryFeed] {
        [.finance, .sports].filter(isVisibleInSettings)
    }

    private func isVisibleInSettings(_ feed: NewsPrimaryFeed) -> Bool {
        guard feed.isAvailable else { return false }
        return switch feed {
        case .wire:
            appModel.feedPreferences.showWire
        case .circle:
            appModel.feedPreferences.showCircle
        case .finance:
            appModel.feedPreferences.showFinance
        case .sports:
            appModel.feedPreferences.showSports
        case .subscribed:
            appModel.feedPreferences.visibleFeeds.contains(.subscribed)
        case .following:
            appModel.feedPreferences.visibleFeeds.contains(.following)
        }
    }

    /// LatrKit owns the archive; the Semble connector has no archived bucket to show.
    private var showsArchiveTab: Bool {
        !appModel.isSembleReadLaterEnabled
            && appModel.visibleReaderListSources.contains(.archive)
    }

    var body: some View {
        Group {
            if !isTabConfigurationLoaded {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                feedTabs
            }
        }
        .task(bootstrap)
        .task(id: NewsDestinationLoadContext(viewerDID: appModel.viewerDID, slot: selectedSlot)) {
            await loadSelectedDestination(selectedSlot)
        }
        .onChange(of: selectedSlot, selectedSlotChanged)
        .onChange(of: selectedSection, selectedSectionChanged)
        .onChange(of: appModel.viewerDID, viewerDidChange)
        .onChange(of: appModel.primaryTabFeeds, primaryTabFeedsChanged)
        .onChange(of: appModel.feedPreferences) { _, _ in
            appModel.loadPrimaryTabPreferences()
            reconcileVisibleSelection()
        }
        .onChange(of: appModel.visiblePrimaryTabFeedChoices) { _, _ in
            appModel.loadPrimaryTabPreferences()
            reconcileVisibleSelection()
        }
        .onChange(of: appModel.feedSelection) { _, selection in
            if case .folder = selection { showSelectedLibraryScope() }
        }
        .onChange(of: showsArchiveTab) { _, showsArchive in
            if !showsArchive, selectedSlot == .archive { selectedSlot = .readLater }
        }
        .onChange(of: availableTabs, availableTabsChanged)
        .onChange(of: sceneModel.selectedTab, sceneTabChanged)
        .onChange(of: appModel.readerListSource, readerListSourceChanged)
        .onChange(of: appModel.selectedSidebar, sidebarSelectionChanged)
        .sheet(isPresented: $isProfilePresented) {
            NavigationStack {
                ProfileView()
            }
        }
    }

    private let showsProfileInSidebar = false

    private var usesExpandedNavigation: Bool {
        #if os(iOS)
        horizontalSizeClass == .regular
        #else
        true
        #endif
    }

    @ViewBuilder
    private var feedTabs: some View {
        if usesExpandedNavigation {
            NavigationSplitView(columnVisibility: $columnVisibility) {
                NewsRootSidebar(
                    selectedSection: selectedSection,
                    selectedSlot: selectedSlot,
                    readLaterItems: readLaterNavigationItems,
                    newsItems: feedDestinations.map(NewsNavigationItem.init(feed:)),
                    topicItems: topicDestinations.map(NewsNavigationItem.init(feed:)),
                    onSelect: selectSidebarSlot,
                    showsProfileButton: showsProfileInSidebar,
                    isProfilePresented: $isProfilePresented
                )
                    .navigationSplitViewColumnWidth(min: 200, ideal: 240, max: 300)
            } detail: {
                if selectedSection == .lists {
                    listsWorkspace
                } else {
                    sectionPager(selectedSection, items: navigationItems(for: selectedSection), usesSidebarNavigation: true)
                }
            }
            .navigationSplitViewStyle(.balanced)
        } else {
            feedTabsContent
                .tabViewStyle(.tabBarOnly)
        }
    }

    private var readLaterNavigationItems: [NewsNavigationItem] {
        [NewsNavigationItem(slot: .readLater, title: "Read Later", systemImage: "bookmark")]
            + (showsArchiveTab ? [NewsNavigationItem(slot: .archive, title: "Archive", systemImage: "archivebox")] : [])
    }

    private var standardListNavigationItems: [NewsNavigationItem] {
        appModel.standardReaderLists.lists.map {
            NewsNavigationItem(slot: .standardList($0.uri), title: $0.name, systemImage: "list.bullet")
        }
    }

    private var feedTabsContent: some View {
        TabView(selection: $selectedSection) {
            Tab("Read Later", systemImage: "bookmark", value: NewsRootSection.readLater) {
                sectionPager(.readLater, items: readLaterNavigationItems)
            }
            Tab("News", systemImage: "newspaper", value: NewsRootSection.feeds) {
                sectionPager(.feeds, items: feedDestinations.map(NewsNavigationItem.init(feed:)))
            }
            if !topicDestinations.isEmpty {
                Tab("Topics", systemImage: "square.grid.2x2", value: NewsRootSection.topics) {
                    sectionPager(.topics, items: topicDestinations.map(NewsNavigationItem.init(feed:)))
                }
            }
            Tab("Lists", systemImage: "list.bullet", value: NewsRootSection.lists) {
                listsWorkspace
            }
        }
    }

    private var listsWorkspace: some View {
        StandardReaderListsWorkspace(isProfilePresented: $isProfilePresented, usesExternalSidebar: usesExpandedNavigation, showsProfileButton: !showsProfileInSidebar)
            .accessibilityIdentifier("news-tab-content-standardLists")
    }

    private func sectionPager(
        _ section: NewsRootSection,
        items: [NewsNavigationItem],
        usesSidebarNavigation: Bool = false
    ) -> some View {
        let selection = sectionSelectionBinding(for: section)
        let slot = selection.wrappedValue
        let feed = slot.primaryFeed
        let tab = slot.savedListSource != nil ? NewsTab.saved : feed?.newsTab ?? .library
        return NewsFeedShellView(
            title: items.first(where: { $0.slot == slot })?.title ?? String(localized: section.title),
            tab: tab,
            sceneModel: sceneModel,
            isProfilePresented: $isProfilePresented,
            supportsUnreadFilter: !items.isEmpty && (feed == .subscribed || feed == .following),
            bulkReadScope: items.isEmpty ? .unavailable : feed.map { bulkReadScope(for: $0) } ?? .unavailable,
            showsProfileButton: !showsProfileInSidebar
        ) {
            NewsSectionPager(items: items, selection: selection, usesSidebarNavigation: usesSidebarNavigation) { page in
                NewsContentColumn(
                    selectedTab: page.savedListSource != nil ? .saved : page.primaryFeed?.newsTab ?? .library,
                    sceneModel: sceneModel
                )
            } emptyContent: {
                ContentUnavailableView(
                    section.emptyTitle,
                    systemImage: section.systemImage,
                    description: Text(section.emptyDescription)
                )
            }
        }
    }

    private func sectionSelectionBinding(for section: NewsRootSection) -> Binding<NewsTabSlot> {
        Binding(
            get: {
                let items = navigationItems(for: section)
                if let remembered = sectionSelections[section], items.contains(where: { $0.slot == remembered }) {
                    return remembered
                }
                return items.first?.slot ?? section.fallbackSlot
            },
            set: { slot in
                sectionSelections[section] = slot
                if selectedSection == section {
                    // A publication/folder shares its root slot with the aggregate feed.
                    // Explicitly tapping that shortcut must still return to the full feed.
                    if let source = slot.savedListSource ?? slot.primaryFeed?.readerListSource,
                       appModel.feedSelection != .topLevel(source) {
                        sceneModel.resetPath(for: slot.primaryFeed?.newsTab ?? .saved)
                        appModel.selectReaderListSource(source)
                    }
                    selectedSlot = slot
                }
            }
        )
    }

    private func navigationItems(for section: NewsRootSection) -> [NewsNavigationItem] {
        switch section {
        case .readLater:
            readLaterNavigationItems
        case .feeds:
            feedDestinations.map(NewsNavigationItem.init(feed:))
        case .topics:
            topicDestinations.map(NewsNavigationItem.init(feed:))
        case .lists:
            standardListNavigationItems
        }
    }

    private func selectSidebarSlot(_ slot: NewsTabSlot) {
        guard let section = rootSection(for: slot) else { return }
        sectionSelections[section] = slot
        selectedSection = section
        let isRepeatedSelection = selectedSlot == slot
        selectedSlot = slot
        if let source = slot.savedListSource ?? slot.primaryFeed?.readerListSource {
            sceneModel.resetPath(for: slot.primaryFeed?.newsTab ?? .saved)
            appModel.selectReaderListSource(source)
            if slot.savedListSource != nil { appModel.clearSavedFeedSource() }
        }
        if isRepeatedSelection, case .standardList(let uri) = slot,
           let list = appModel.standardReaderLists.lists.first(where: { $0.uri == uri }) {
            Task { await appModel.standardReaderLists.select(list) }
        }
    }

    private func selectedSectionChanged(_ oldValue: NewsRootSection, _ section: NewsRootSection) {
        guard section != .lists else { return }
        reconcileSectionSelection(section)
    }

    private func reconcileSectionSelection(_ section: NewsRootSection) {
        let items = navigationItems(for: section)
        guard let slot = sectionSelections[section].flatMap({ remembered in
            items.first(where: { $0.slot == remembered })?.slot
        }) ?? items.first?.slot else { return }
        sectionSelections[section] = slot
        if selectedSection == section, selectedSlot != slot {
            selectedSlot = slot
        }
    }

    private func bootstrap() async {
        guard !isTabConfigurationLoaded else { return }
        sceneModel.configureWindow(identifier: windowID)
        if let viewerDID = appModel.viewerDID,
           let preferences = ReaderFeedPreferencesStorage.load(viewerDid: viewerDID) {
            appModel.feedPreferences = preferences
        }
        appModel.loadPrimaryTabPreferences()
        slotFeeds = appModel.primaryTabFeeds
        sceneModel.updateContext(viewerDID: appModel.viewerDID, availableTabs: availableTabs)

        if let viewerDID = appModel.viewerDID,
           let lastFeed = NewsPrimaryFeedStorage.lastFeed(viewerDID: viewerDID),
           slotFeeds.contains(lastFeed) {
            selectPrimaryFeed(lastFeed)
        } else if let firstFeed = slotFeeds.first {
            selectedSlot = NewsTabSlot(firstFeed)
            activatePrimaryFeed(firstFeed)
        } else {
            selectedSlot = .readLater
            sceneModel.prepareForReaderSourceChange(from: appModel.readerListSource, to: .readLater)
            appModel.selectReaderListSource(.readLater)
            sceneModel.select(.saved, availableTabs: availableTabs)
        }
        isTabConfigurationLoaded = true
    }

    private func selectedSlotChanged(_ oldValue: NewsTabSlot, _ slot: NewsTabSlot) {
        if let section = rootSection(for: slot) {
            sectionSelections[section] = slot
            if selectedSection != section {
                selectedSection = section
            }
        }
        if let source = slot.savedListSource {
            // Read Later and Archive are separate tabs now, so each owns its own source.
            sceneModel.prepareForReaderSourceChange(from: appModel.readerListSource, to: source)
            if appModel.readerListSource != source {
                appModel.selectReaderListSource(source)
            }
            sceneModel.select(.saved, availableTabs: availableTabs)
        } else if case let .standardList(uri) = slot,
                  let list = appModel.standardReaderLists.lists.first(where: { $0.uri == uri }) {
            Task { await appModel.standardReaderLists.select(list) }
        } else if let feed = slot.primaryFeed {
            activatePrimaryFeed(feed)
        }
    }

    private func loadSelectedDestination(_ slot: NewsTabSlot) async {
        guard let feed = slot.primaryFeed, feed.isAvailable else { return }
        switch feed {
        case .wire:
            if appModel.wireCatalog == nil {
                await appModel.refreshWireCatalog()
            }
            await appModel.loadWireEdition()
        case .circle:
            if appModel.circleCatalog == nil {
                await appModel.refreshCircleCatalog()
            }
            await appModel.loadCircleEdition()
        case .finance:
            async let feeds: Void = appModel.loadFinanceFeeds()
            // Resolve interests before the story request captures its preference
            // fingerprint; a concurrent selection load can otherwise discard it.
            guard await appModel.loadFinanceSelections(), !Task.isCancelled else { return }
            await feeds
            guard !Task.isCancelled else { return }
            async let customization: Void = appModel.loadFinanceCustomization(reconcilingSelections: false)
            async let stories: Void = appModel.loadFinance()
            _ = await (customization, stories)
        case .sports:
            let topic = appModel.sportsTopic
            topic.bind(viewer: appModel.viewerDID)
            await topic.loadCatalog()
            await topic.refresh(language: Locale.current.language.languageCode?.identifier ?? "en")
        case .subscribed, .following:
            break
        }
    }

    private func rootSection(for slot: NewsTabSlot) -> NewsRootSection? {
        if slot.savedListSource != nil { return .readLater }
        if case .standardList = slot { return .lists }
        guard let feed = slot.primaryFeed else { return nil }
        return [.finance, .sports].contains(feed) ? .topics : .feeds
    }

    private func viewerDidChange(_ oldValue: String?, _ viewerDID: String?) {
        isTabConfigurationLoaded = false
        sceneModel.updateContext(viewerDID: viewerDID, availableTabs: availableTabs)
        appModel.loadPrimaryTabPreferences()
        slotFeeds = appModel.primaryTabFeeds
        isTabConfigurationLoaded = true
    }

    private func primaryTabFeedsChanged(
        _ oldValue: [NewsPrimaryFeed],
        _ feeds: [NewsPrimaryFeed]
    ) {
        slotFeeds = feeds
        guard selectedSection != .lists else { return }

        if let selectedFeed = selectedSlot.primaryFeed,
           !appModel.visiblePrimaryTabFeedChoices.contains(selectedFeed) {
            if let firstFeed = navigableFeeds.first {
                selectPrimaryFeed(firstFeed)
            } else {
                selectedSlot = .readLater
            }
        } else if let activePrimaryFeed {
            activatePrimaryFeed(activePrimaryFeed)
        }
    }

    private func availableTabsChanged(_ oldValue: [NewsTab], _ tabs: [NewsTab]) {
        sceneModel.updateContext(viewerDID: appModel.viewerDID, availableTabs: tabs)
    }

    private func sceneTabChanged(_ oldValue: NewsTab, _ tab: NewsTab) {
        guard isTabConfigurationLoaded, selectedSection != .lists else { return }
        switch tab {
        case .wire:
            selectPrimaryFeed(.wire)
        case .finance:
            selectPrimaryFeed(.finance)
        case .sports:
            selectPrimaryFeed(.sports)
        case .circle:
            selectPrimaryFeed(.circle)
        case .library:
            // A disappearing discovery tab can select Library before the reader source
            // has changed. Never turn that fallback into a hidden Wire tab.
            let preferred: NewsPrimaryFeed = appModel.readerListSource == .following ? .following : .subscribed
            let choices = appModel.visiblePrimaryTabFeedChoices
            if choices.contains(preferred) {
                selectPrimaryFeed(preferred)
            } else if let feed = choices.first(where: { $0 == .subscribed || $0 == .following }) {
                selectPrimaryFeed(feed)
            } else {
                selectedSlot = .readLater
            }
        case .saved:
            selectedSlot = appModel.readerListSource == .archive && showsArchiveTab
                ? .archive
                : .readLater
        case .search:
            break
        }
    }

    private func readerListSourceChanged(
        _ oldValue: ReaderListSource,
        _ source: ReaderListSource
    ) {
        guard isTabConfigurationLoaded,
              selectedSection != .lists,
              sceneModel.selectedTab == .library,
              let feed = primaryFeed(for: source)
        else { return }
        selectPrimaryFeed(feed)
    }

    private func sidebarSelectionChanged(_ oldValue: SidebarSelection?, _ selection: SidebarSelection?) {
        guard isTabConfigurationLoaded else { return }
        switch selection {
        case .publication:
            showSelectedLibraryScope()
        case .myPublications:
            selectPrimaryFeed(.subscribed)
            sceneModel.resetPath(for: .library)
        default:
            break
        }
    }

    private func showSelectedLibraryScope() {
        guard isTabConfigurationLoaded else { return }
        // Moving into Library must not invoke selectReaderListSource: that resets
        // the publication/folder which the sidebar or Profile just selected.
        let source: ReaderListSource = appModel.readerListSource == .following ? .following : .subscribed
        appModel.readerListSource = source
        appModel.publicationSidebarTab = source == .following ? .following : .subscribed
        ReaderListSourceStorage.save(source)
        selectPrimaryFeed(source == .following ? .following : .subscribed)
        sceneModel.resetPath(for: .library)
    }

    private func reconcileVisibleSelection() {
        guard isTabConfigurationLoaded else { return }
        if selectedSection == .topics, topicDestinations.isEmpty { selectedSection = .feeds }
        if selectedSlot == .archive, !showsArchiveTab {
            selectedSlot = .readLater
        }
        primaryTabFeedsChanged(slotFeeds, appModel.primaryTabFeeds)
    }

    private func selectPrimaryFeed(_ feed: NewsPrimaryFeed) {
        selectedSlot = NewsTabSlot(feed)
        activatePrimaryFeed(feed)
    }

    private func activatePrimaryFeed(_ feed: NewsPrimaryFeed) {
        if let source = feed.readerListSource,
           appModel.readerListSource != source {
            sceneModel.prepareForReaderSourceChange(from: appModel.readerListSource, to: source)
            appModel.selectReaderListSource(source)
        }
        sceneModel.select(feed.newsTab, availableTabs: availableTabs)
        if let viewerDID = appModel.viewerDID {
            NewsPrimaryFeedStorage.saveLastFeed(feed, viewerDID: viewerDID)
        }
    }

    private func primaryFeed(for source: ReaderListSource) -> NewsPrimaryFeed? {
        switch source {
        case .wire:
            .wire
        case .subscribed:
            .subscribed
        case .following:
            .following
        case .readLater, .archive:
            nil
        }
    }

    private func bulkReadScope(for feed: NewsPrimaryFeed) -> ReaderMarkReadScope {
        switch feed {
        case .subscribed, .following:
            return ReaderMarkReadScope.selectedFeed(appModel.feedSelection)
        case .wire, .circle, .finance, .sports:
            return .unavailable
        }
    }
}
