import SwiftUI

struct NewsRootSidebar: View {
    @Environment(SocialWireAppModel.self) private var appModel
    let selectedSection: NewsRootSection
    let selectedSlot: NewsTabSlot
    let readLaterItems: [NewsNavigationItem]
    let newsItems: [NewsNavigationItem]
    let topicItems: [NewsNavigationItem]
    let onSelect: (NewsTabSlot) -> Void
    let showsProfileButton: Bool
    @Binding var isProfilePresented: Bool

    private func loadFinanceNavigation() async {
        guard topicItems.contains(where: { $0.slot == .finance }) else { return }
        await appModel.loadFinanceFeeds()
        _ = await appModel.loadFinanceSelections()
    }

    private func loadSportsNavigation() async {
        guard topicItems.contains(where: { $0.slot == .sports }) else { return }
        let topic = appModel.sportsTopic
        topic.bind(viewer: appModel.viewerDID)
        await topic.loadCatalog()
        do {
            try await topic.reconcileSelections()
        } catch {
            appModel.errorMessage = "Couldn’t Load Sports Interests. \(error.localizedDescription)"
        }
    }

    var body: some View {
        List {
            Section("Read Later") {
                ForEach(readLaterItems) { item in
                    NewsSidebarFeedRow(item: item, isSelected: selectedSection == .readLater && selectedSlot == item.slot, onSelect: onSelect)
                }
            }
            Section("News") {
                ForEach(newsItems) { item in
                    NewsSidebarFeedRow(item: item, isSelected: selectedSection == .feeds && selectedSlot == item.slot, onSelect: onSelect)
                }
            }
            if !topicItems.isEmpty {
                Section("Topics") {
                    ForEach(topicItems) { item in
                        NewsSidebarFeedRow(item: item, isSelected: selectedSection == .topics && selectedSlot == item.slot, onSelect: onSelect)
                    }
                }
            }
            Section("Lists") {
                ForEach(appModel.standardReaderLists.lists) { list in
                    NewsSidebarButton(
                        title: list.name, systemImage: "list.bullet",
                        isSelected: selectedSection == .lists && appModel.standardReaderLists.selectedList?.uri == list.uri,
                        identifier: "news-sidebar-list-\(list.uri)"
                    ) {
                        onSelect(.standardList(list.uri))
                    }
                }
                if appModel.standardReaderLists.lists.isEmpty {
                    NewsSidebarButton(title: "Manage Lists", systemImage: "list.bullet", isSelected: selectedSection == .lists, identifier: "news-sidebar-lists") {
                        onSelect(.standardList(""))
                    }
                }
            }
        }
        .listStyle(.sidebar)
        .navigationTitle("The Social Wire")
        .accessibilityIdentifier("news-root-sidebar")
        .toolbar {
            if showsProfileButton {
                ToolbarItem(placement: .primaryAction) {
                    Button {
                        isProfilePresented = true
                    } label: {
                        ViewerProfileAvatar(size: 30)
                    }
                    .accessibilityLabel("Profile")
                }
            }
        }
        .task(id: appModel.viewerDID) {
            async let lists: Void = appModel.standardReaderLists.load()
            async let finance: Void = loadFinanceNavigation()
            async let sports: Void = loadSportsNavigation()
            _ = await (lists, finance, sports)
        }
    }
}
