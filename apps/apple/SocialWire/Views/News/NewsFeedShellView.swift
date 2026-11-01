import SwiftUI

struct NewsFeedShellView<Content: View>: View {
    @Environment(SocialWireAppModel.self) private var appModel
    @State private var presentedSheet: NewsShellSheet?

    let title: String
    let tab: NewsTab
    let sceneModel: NewsSceneModel
    @Binding var isProfilePresented: Bool
    let supportsUnreadFilter: Bool
    let bulkReadScope: ReaderMarkReadScope
    let showsProfileButton: Bool
    @ViewBuilder let content: () -> Content

    private enum NewsShellSheet: String, Identifiable {
        case addPublication
        case newFolder
        case importOPML

        var id: String { rawValue }
    }

    private var effectiveTitle: String {
        if tab == .finance {
            return appModel.selectedFinanceFeed.title
        }
        if tab == .sports {
            return appModel.sportsTopic.selectedTitle
        }
        if tab == .library, let publication = appModel.selectedPublication {
            return publication.title
        }
        return title
    }

    var body: some View {
        NavigationStack(path: pathBinding) {
            content()
                .navigationTitle(effectiveTitle)
                #if os(iOS)
                .navigationBarTitleDisplayMode(.inline)
                #endif
                .toolbar { toolbarContent }
                .navigationDestination(for: NewsRoute.self) { route in
                    NewsRouteDestination(route: route, tab: tab, sceneModel: sceneModel)

                }
        }
        .sheet(item: $presentedSheet) { sheet in
            switch sheet {
            case .addPublication:
                AddPublicationView()
            case .newFolder:
                NewFolderView()
            case .importOPML:
                OPMLImportView()
            }
        }
    }

    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        if showsProfileButton {
            ToolbarItem(placement: trailingPlacement) {
                Button {
                    isProfilePresented = true
                } label: {
                    ViewerProfileAvatar(size: 30)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Profile")
            }
        }
        if tab == .library && appModel.readerListSource == .subscribed {
            ToolbarItem(placement: trailingPlacement) {
                Menu {
                    Button("Add Publication", systemImage: "plus.circle") {
                        presentedSheet = .addPublication
                    }
                    Button("New Folder", systemImage: "folder.badge.plus") {
                        presentedSheet = .newFolder
                    }
                    Button("Import OPML", systemImage: "square.and.arrow.down") {
                        presentedSheet = .importOPML
                    }
                } label: {
                    Label("Add", systemImage: "plus")
                }
                .help("Add a publication, folder, or OPML subscription list")
            }
        }
        if supportsUnreadFilter {
            ToolbarItem(placement: trailingPlacement) {
                Button("Filter Unread", systemImage: unreadFilterSystemImage) {
                    Task {
                        await appModel.applyReaderFilter(appModel.readerFilter == .unread ? .all : .unread)
                    }
                }
                .accessibilityValue(appModel.readerFilter == .unread ? "On" : "Off")
            }
        }
        if bulkReadScope != .unavailable {
            ToolbarItem(placement: trailingPlacement) {
                FeedMarkReadButton(
                    contextID: "\(appModel.viewerDID ?? ""):news:\(bulkReadScope)",
                    refreshRevision: appModel.readAgeRevision,
                    scopeTitle: bulkReadTitle,
                    loadOptions: { onOptions in
                        try await appModel.readAgeOptions(for: bulkReadScope, onOptions: onOptions)
                    },
                    markAllRead: { await appModel.markRead(for: bulkReadScope) },
                    markOlderRead: { try await appModel.markRead(for: bulkReadScope, before: $0.before) },
                    markAllUnread: { await appModel.markUnread(for: bulkReadScope) }
                )
            }
        }
    }

    /// The bar placements differ by platform; macOS has no top bar edges.
    private var leadingPlacement: ToolbarItemPlacement {
        #if os(macOS)
        .navigation
        #else
        .topBarLeading
        #endif
    }

    private var trailingPlacement: ToolbarItemPlacement {
        #if os(macOS)
        .primaryAction
        #else
        .topBarTrailing
        #endif
    }

    private var pathBinding: Binding<[NewsRoute]> {
        Binding(
            get: { sceneModel.path(for: tab) },
            set: { sceneModel.setPath($0, for: tab) }
        )
    }

    private var unreadFilterSystemImage: String {
        appModel.readerFilter == .unread
            ? "line.3.horizontal.decrease.circle.fill"
            : "line.3.horizontal.decrease.circle"
    }

    private var bulkReadTitle: String {
        switch bulkReadScope {
        case .publication(let id):
            appModel.publication(forId: id)?.title ?? "This Publication"
        case .folder(let key):
            appModel.folders.first { $0.uri.hasSuffix("/\(key)") }?.value.name ?? "This Folder"
        default:
            effectiveTitle
        }
    }

}
