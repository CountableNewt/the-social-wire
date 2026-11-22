import Foundation
import SwiftData

enum ReaderSwiftDataStack {
    private static var models: [any PersistentModel.Type] {
        [PersistedGatewayResponse.self, PersistedPublicationEntries.self, PersistedEntryDetail.self]
    }

    static func makeReaderContainer() throws -> ModelContainer {
        let schema = Schema(models)
        guard let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first else {
            throw SocialWireError.badResponse("Application Support unavailable.")
        }
        let folder = appSupport.appendingPathComponent("SocialWire", isDirectory: true)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let url = folder.appendingPathComponent("reader-cache.store")
        let config = ModelConfiguration(schema: schema, url: url)
        do {
            return try ModelContainer(for: schema, configurations: [config])
        } catch {
            // The reader cache is rebuildable; discard a store that cannot migrate.
            for suffix in ["", "-shm", "-wal"] {
                try? FileManager.default.removeItem(at: URL(fileURLWithPath: url.path + suffix))
            }
            return try ModelContainer(for: schema, configurations: [config])
        }
    }

    static func inMemoryTestContainer() throws -> ModelContainer {
        let schema = Schema(models)
        let config = ModelConfiguration(schema: schema, isStoredInMemoryOnly: true)
        return try ModelContainer(for: schema, configurations: [config])
    }
}
