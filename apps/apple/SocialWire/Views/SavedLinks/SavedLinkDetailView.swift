import SwiftUI

struct SavedLinkDetailView: View {
    @Environment(SocialWireAppModel.self) private var appModel

    let save: MergedLatrSave

    private var isArchivedView: Bool {
        appModel.readerListSource == .archive || save.state == "archived"
    }

    var body: some View {
        Group {
            if let url = save.url {
                VStack(spacing: 0) {
                    HStack(spacing: 12) {
                        ShareLink(item: url) {
                            Label("Share", systemImage: "square.and.arrow.up")
                        }
                        .buttonStyle(.bordered)

                        Link(destination: url) {
                            Label("Open", systemImage: "safari")
                        }
                        .buttonStyle(.bordered)

                        if isArchivedView {
                            Button {
                                Task { await appModel.unarchive(save) }
                            } label: {
                                Label("Unarchive", systemImage: "arrow.uturn.backward")
                            }
                            .buttonStyle(.bordered)
                        } else {
                            Button {
                                Task { await appModel.archive(save) }
                            } label: {
                                Label("Archive", systemImage: "archivebox")
                            }
                            .buttonStyle(.bordered)
                        }

                        Button(role: .destructive) {
                            Task { await appModel.delete(save) }
                        } label: {
                            Label("Delete", systemImage: "trash")
                        }
                        .buttonStyle(.bordered)

                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal)
                    .padding(.vertical, 8)

                    WebPreview(url: url)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            } else {
                ContentUnavailableView(
                    "Preview Unavailable",
                    systemImage: "archivebox",
                    description: Text("Native ATProto saved item previews are not available yet.")
                )
            }
        }
    }
}
