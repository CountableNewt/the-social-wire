import SwiftUI

struct NewsStartupSettingsSection: View {
    @AppStorage private var destinationRaw: String

    init(viewerDID: String) {
        _destinationRaw = AppStorage(
            wrappedValue: NewsStartupDestination.lastUsed.rawValue,
            NewsStartupDestination.key(viewerDID: viewerDID)
        )
    }

    var body: some View {
        Section {
            Picker("Open On Launch", selection: $destinationRaw) {
                ForEach(NewsStartupDestination.allCases) { destination in
                    Text(destination.title).tag(destination.rawValue)
                }
            }
        } header: {
            Text("Startup")
        } footer: {
            Text("Choose where this account opens on this device. Changes apply the next time you launch the app. Hidden feeds fall back to an available feed.")
        }
    }
}
