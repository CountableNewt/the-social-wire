import SwiftData
import SwiftUI

struct RootView: View {
    @Environment(SocialWireAppModel.self) private var appModel
    @Environment(\.modelContext) private var modelContext
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        Group {
            if appModel.isSignedIn {
                MainSplitView()
            } else {
                LoginView()
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(.systemBackground))
        .task {
            appModel.configureReaderPersistence(modelContext: modelContext)
            await appModel.restoreSession()
        }
        .onChange(of: scenePhase) { _, phase in
            guard phase == .active, appModel.isSignedIn else { return }
            Task { await appModel.syncCrossClientReadState() }
        }
        .alert("Something went wrong", isPresented: Binding(
            get: { appModel.errorMessage != nil },
            set: { if !$0 { appModel.errorMessage = nil } }
        )) {
            Button("OK", role: .cancel) { appModel.errorMessage = nil }
        } message: {
            Text(appModel.errorMessage ?? "Unknown error")
        }
    }
}
