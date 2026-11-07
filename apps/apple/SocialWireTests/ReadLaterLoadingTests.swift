import Foundation
import SwiftData
import Testing
@testable import SocialWire

@Suite("Read Later loading")
@MainActor
struct ReadLaterLoadingTests {
    @Test("overlapping passive loads share work and recent loads are reused")
    func coalescesPassiveRefreshes() async {
        let coordinator = SavedLinksRefreshCoordinator()
        let (gate, continuation) = AsyncStream<Void>.makeStream()
        var calls = 0
        let first = Task {
            await coordinator.refresh(force: false) {
                calls += 1
                for await _ in gate { break }
                return true
            }
        }
        while calls == 0 { await Task.yield() }
        var joined = false
        let second = Task {
            joined = true
            await coordinator.refresh(force: false) {
                calls += 1
                return true
            }
        }
        while !joined { await Task.yield() }
        continuation.yield(())
        continuation.finish()
        await first.value
        await second.value
        await coordinator.refresh(force: false) {
            calls += 1
            return true
        }
        #expect(calls == 1)
    }

    @Test("a mutation during refresh queues a fresh pass")
    func mutationDuringRefresh() async {
        let coordinator = SavedLinksRefreshCoordinator()
        let (gate, continuation) = AsyncStream<Void>.makeStream()
        var calls = 0
        let first = Task {
            await coordinator.refresh(force: false) {
                calls += 1
                if calls == 1 {
                    for await _ in gate { break }
                }
                return true
            }
        }
        while calls == 0 { await Task.yield() }
        var joined = false
        let mutation = Task {
            joined = true
            await coordinator.refresh(force: true) { true }
        }
        while !joined { await Task.yield() }
        continuation.yield(())
        continuation.finish()
        await first.value
        await mutation.value
        #expect(calls == 2)
    }

    @Test("failed loads remain retryable and reset clears freshness")
    func failedRefreshAndReset() async {
        let coordinator = SavedLinksRefreshCoordinator()
        var calls = 0
        await coordinator.refresh(force: false) { calls += 1; return false }
        await coordinator.refresh(force: false) { calls += 1; return true }
        coordinator.reset()
        await coordinator.refresh(force: false) { calls += 1; return true }
        #expect(calls == 3)
    }

    @Test("preview metadata survives SwiftData reload without crossing viewers")
    func persistedPreviews() throws {
        let container = try ReaderSwiftDataStack.inMemoryTestContainer()
        let first = ReaderCacheCoordinator(modelContext: ModelContext(container))
        var cache = SavedLinkPreviewCache()
        let now = Date()
        let metadata = LatrSaveMetadata(title: "Story", image: "https://example.com/photo.jpg")
        cache.store(metadata, for: "bookmark", now: now)
        let key = SavedLinkPreviewCache.storageKey(viewerDID: "did:plc:one")
        try first.upsertGatewayResponse(cacheKey: key, etag: nil, body: JSONEncoder().encode(cache))

        let reloaded = ReaderCacheCoordinator(modelContext: ModelContext(container))
        let body = try #require(reloaded.gatewayCachedBody(for: key))
        let restored = try JSONDecoder().decode(SavedLinkPreviewCache.self, from: body)
        #expect(restored.metadata(for: "bookmark", now: now) == metadata)
        #expect(restored.metadata(for: "bookmark", now: now.addingTimeInterval(SavedLinkPreviewCache.lifetime + 1)) == nil)
        #expect(reloaded.gatewayCachedBody(for: SavedLinkPreviewCache.storageKey(viewerDID: "did:plc:two")) == nil)
    }

    @Test("preview persistence keeps only the newest bounded entries")
    func boundedPreviewCache() {
        var cache = SavedLinkPreviewCache()
        let start = Date()
        for index in 0 ... SavedLinkPreviewCache.maximumEntries {
            cache.store(LatrSaveMetadata(title: "Story"), for: "\(index)", now: start.addingTimeInterval(Double(index)))
        }
        let now = start.addingTimeInterval(300)
        #expect(cache.metadata(for: "0", now: now) == nil)
        #expect(cache.metadata(for: "\(SavedLinkPreviewCache.maximumEntries)", now: now) != nil)
    }

    @Test("saved list snapshots persist rows, stay account scoped, expire, and accept empty refreshes")
    func savedListSnapshots() throws {
        let container = try ReaderSwiftDataStack.inMemoryTestContainer()
        let coordinator = ReaderCacheCoordinator(modelContext: ModelContext(container))
        let now = Date()
        let save = MergedLatrSave.external(MergedLatrExternalSave(
            normalizedUrl: "https://example.com/story", url: "https://example.com/story",
            savedAt: "2026-11-05T00:00:00Z", externalRkey: "external", itemRkey: "item",
            externalUri: "at://did:plc:one/external/item", itemUri: "at://did:plc:one/bookmark/item",
            subjectUri: "at://did:plc:publisher/article/story", state: "unread", title: "Cached Story",
            image: "https://example.com/photo.jpg", tags: ["News"]
        ))
        let snapshot = SavedLinksSnapshot(active: Array(repeating: save, count: 130), archived: [save.withState("archived")], now: now)
        #expect(snapshot.active.count == 120)
        let key = SavedLinksSnapshot.storageKey(viewerDID: "did:plc:one")
        try coordinator.upsertGatewayResponse(cacheKey: key, etag: nil, body: JSONEncoder().encode(snapshot))
        let reloaded = ReaderCacheCoordinator(modelContext: ModelContext(container))
        let body = try #require(reloaded.gatewayCachedBody(for: key))
        let restored = try JSONDecoder().decode(SavedLinksSnapshot.self, from: body)
        #expect(restored.active.first == save)
        #expect(restored.archived.first?.state == "archived")
        #expect(restored.isUsable(now: now))
        #expect(!restored.isUsable(now: now.addingTimeInterval(7 * 24 * 60 * 60)))
        #expect(reloaded.gatewayCachedBody(for: SavedLinksSnapshot.storageKey(viewerDID: "did:plc:two")) == nil)
        try reloaded.upsertGatewayResponse(cacheKey: key, etag: nil, body: JSONEncoder().encode(SavedLinksSnapshot(active: [], archived: [])))
        let emptyBody = try #require(reloaded.gatewayCachedBody(for: key))
        let empty = try JSONDecoder().decode(SavedLinksSnapshot.self, from: emptyBody)
        #expect(empty.active.isEmpty && empty.archived.isEmpty)
    }

    @Test("receipt reuse respects expiry and token or account changes")
    func receiptReuse() throws {
        let now = Date(timeIntervalSince1970: 1_000)
        let session = AuthSession(
            did: "did:plc:one",
            pdsURL: try #require(URL(string: "https://pds.example")),
            tokenEndpoint: try #require(URL(string: "https://pds.example/token")),
            accessToken: "one", refreshToken: "refresh", tokenType: "DPoP",
            expiresAt: now.addingTimeInterval(300)
        )
        let body = try JSONSerialization.data(withJSONObject: ["exp": 1_060]).base64URLEncodedString()
        let receipt = "\(body).signature"
        var cache = GatewaySessionReceiptCache()
        cache.store(receipt, for: session)
        #expect(cache.value(for: session, now: now) == receipt)
        #expect(cache.value(for: session, now: now.addingTimeInterval(56)) == nil)
        var changed = session
        changed.accessToken = "two"
        #expect(cache.value(for: changed, now: now) == nil)
        changed = session
        changed.did = "did:plc:two"
        #expect(cache.value(for: changed, now: now) == nil)
        cache.store("malformed", for: session)
        #expect(cache.value(for: session, now: now) == nil)
    }
}
