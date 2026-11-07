import Foundation

/// Shares overlapping loads; mutations during a load request one more pass.
@MainActor
final class SavedLinksRefreshCoordinator {
    private var task: Task<Void, Never>?
    private var generation = UUID()
    private var refreshAgain = false
    private var lastSuccess: Date?

    func refresh(force: Bool, load: @escaping @MainActor () async -> Bool) async {
        if let task {
            if force { refreshAgain = true }
            await task.value
            return
        }
        if !force, let lastSuccess, Date().timeIntervalSince(lastSuccess) < 30 { return }
        let currentGeneration = generation
        let task = Task { @MainActor in
            defer {
                if generation == currentGeneration { self.task = nil }
            }
            repeat {
                refreshAgain = false
                let succeeded = await load()
                guard !Task.isCancelled, generation == currentGeneration else { return }
                if succeeded { lastSuccess = Date() }
            } while refreshAgain
        }
        self.task = task
        await task.value
    }

    func reset() {
        generation = UUID()
        task?.cancel()
        task = nil
        refreshAgain = false
        lastSuccess = nil
    }
}
