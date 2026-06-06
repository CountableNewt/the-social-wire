import SwiftUI

struct PublicationsPaneView: View {
    @Environment(SocialWireAppModel.self) private var appModel
    @Binding var showingNewFolder: Bool
    @Binding var showingAddPublication: Bool
    var onPublicationTap: ((DiscoveredPublication) -> Void)? = nil
    var onSavedLinkTap: ((MergedLatrSave) -> Void)? = nil

    var body: some View {
        @Bindable var model = appModel

        Group {
            switch appModel.readerListSource {
            case .readLater, .archive:
                SavedLinksListContent(onSavedLinkTap: onSavedLinkTap)
            case .subscribed:
                List(selection: $model.selectedSidebar) {
                    SubscribedPublicationSidebarTree(
                        showingNewFolder: $showingNewFolder,
                        showingAddPublication: $showingAddPublication,
                        onPublicationTap: onPublicationTap
                    )
                }
                .readerListCanvas()
            case .following:
                List(selection: $model.selectedSidebar) {
                    FollowingPublicationSidebarTree(onPublicationTap: onPublicationTap)
                }
                .readerListCanvas()
            }
        }
    }
}

/// Read-later rows for the publications pane (no local toolbar).
struct SavedLinksListContent: View {
    @Environment(SocialWireAppModel.self) private var appModel
    var onSavedLinkTap: ((MergedLatrSave) -> Void)? = nil

    private var isArchivedView: Bool {
        appModel.readerListSource == .archive
    }

    private var savedListUnavailableDescription: Text {
        let chosen = ReadLaterServiceCatalog.label(for: appModel.effectiveReadLaterServiceId)
        return Text("""
            L@tr Link merges HTTPS read-later URLs from your PDS. You have \(chosen) selected. \
            Use that provider in its own app or site for now, or choose L@tr Link under Read Later in Settings.
            """)
    }

    var body: some View {
        Group {
            if appModel.readLaterLatrConfigured {
                List {
                    if appModel.currentSavedLinks.isEmpty {
                        ContentUnavailableView(
                            isArchivedView ? "Nothing Archived Yet" : "Nothing Queued Yet",
                            systemImage: isArchivedView ? "archivebox" : "bookmark",
                            description: Text(
                                isArchivedView
                                    ? "Archived read-later links will appear here."
                                    : "Save an article from the toolbar or article list to queue it here."
                            )
                        )
                        .readerClearListRow()
                    } else {
                        ForEach(appModel.currentSavedLinks) { save in
                            Button {
                                if let onSavedLinkTap {
                                    onSavedLinkTap(save)
                                } else {
                                    appModel.selectedSavedLink = save
                                }
                            } label: {
                                SavedLinkRow(
                                    save: save,
                                    isSelected: appModel.selectedSavedLink?.id == save.id
                                )
                            }
                            .buttonStyle(.plain)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .readerFullWidthCardRow()
                            .contextMenu {
                                if isArchivedView {
                                    Button("Unarchive") {
                                        Task { await appModel.unarchive(save) }
                                    }
                                } else {
                                    Button("Archive") {
                                        Task { await appModel.archive(save) }
                                    }
                                }
                                Button("Delete", role: .destructive) {
                                    Task { await appModel.delete(save) }
                                }
                            }
                            .swipeActions {
                                if isArchivedView {
                                    Button("Unarchive") {
                                        Task { await appModel.unarchive(save) }
                                    }
                                    .tint(.blue)
                                } else {
                                    Button("Archive") {
                                        Task { await appModel.archive(save) }
                                    }
                                    .tint(.orange)
                                }
                                Button("Delete", role: .destructive) {
                                    Task { await appModel.delete(save) }
                                }
                            }
                        }
                    }
                }
                .listStyle(.plain)
                .scrollContentBackground(.hidden)
                .contentMargins(.bottom, 12, for: .scrollContent)
            } else {
                ContentUnavailableView(
                    "Read-Later List Unavailable",
                    systemImage: "link.badge.plus",
                    description: savedListUnavailableDescription
                )
            }
        }
    }
}
