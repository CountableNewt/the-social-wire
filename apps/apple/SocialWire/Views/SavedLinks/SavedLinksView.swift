import SwiftUI

struct SavedLinksView: View {
    @Environment(SocialWireAppModel.self) private var appModel

    private var savedListUnavailableDescription: Text {
        let chosen = ReadLaterServiceCatalog.label(for: appModel.effectiveReadLaterServiceId)
        return Text("""
            L@tr Link merges HTTPS read-later URLs from your PDS. You have \(chosen) selected. \
            Use that provider in its own app or site for now, or choose L@tr Link under Read Later in Settings.
            """)
    }

    var body: some View {
        @Bindable var model = appModel

        Group {
            if appModel.readLaterLatrConfigured {
                savedLinksMainList(model: model)
            } else {
                readLaterMisconfiguredPlaceholder
            }
        }
        .navigationTitle("Saved Links")
    }

    @ViewBuilder
    private func savedLinksMainList(@Bindable model: SocialWireAppModel) -> some View {
        SavedLinksListContent()
    }

    @ViewBuilder
    private var readLaterMisconfiguredPlaceholder: some View {
        ContentUnavailableView(
            "Read-Later List Unavailable",
            systemImage: "link.badge.plus",
            description: savedListUnavailableDescription
        )
    }
}
