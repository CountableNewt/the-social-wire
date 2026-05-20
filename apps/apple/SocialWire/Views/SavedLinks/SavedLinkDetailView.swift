import SwiftUI

struct SavedLinkDetailView: View {
    @Environment(SocialWireAppModel.self) private var appModel

    let save: MergedLatrSave

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
