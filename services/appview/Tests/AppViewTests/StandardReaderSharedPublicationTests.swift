import Foundation
import Testing
@testable import AppView

struct StandardReaderSharedPublicationTests {
  private let viewer = "did:plc:viewer"
  private let publication = "at://did:plc:publisher/site.standard.publication/news"
  private let slowPublication = "at://did:plc:publisher/site.standard.publication/slow"

  @Test("concurrent lists reuse a shared publication before an unrelated slow batch member finishes")
  func concurrentSharedMembership() async {
    let reader = SharedPublicationReader(blockedURI: slowPublication)
    let service = StandardReaderListsService(reader: reader)
    let firstList = list("first", publications: [publication, slowPublication])
    let secondList = list("second", publications: [publication])
    let first = Task { await service.resolvedScopes(list: firstList, viewerDid: viewer) }
    await reader.waitForCalls(2)
    // The first list is still blocked on its other publication. A second list
    // must share the completed/in-flight read instead of refetching its member.
    let secondScopes = await service.resolvedScopes(list: secondList, viewerDid: viewer)
    #expect(await reader.calls(for: publication) == 1)
    #expect(secondScopes.map(\.publicationId) == [publication])
    #expect(secondScopes.first?.publicationSiteUrls == ["https://journal.example"])
    await reader.release()
    let firstScopes = await first.value
    #expect(firstScopes.map(\.publicationId) == [publication, slowPublication])
    #expect(await reader.totalCalls == 2)
    _ = await service.resolvedScopes(list: secondList, viewerDid: viewer)
    #expect(await reader.calls(for: publication) == 1)
  }

  @Test("forced refresh never joins an older lookup or lets it overwrite refreshed publication metadata")
  func refreshSeparatesInFlightGenerations() async throws {
    let reader = SharedPublicationReader(blockedURI: publication, blockFirstOnly: true)
    let service = StandardReaderListsService(reader: reader)
    let snapshot = list("first", publications: [publication])
    let old = Task { await service.resolvedScopes(list: snapshot, viewerDid: viewer) }
    await reader.waitForCalls(1)
    _ = try await service.lists(viewerDid: viewer, refresh: true)
    let fresh = await service.resolvedScopes(list: snapshot, viewerDid: viewer)
    #expect(fresh.first?.publicationSiteUrls == ["https://refreshed.example"])
    #expect(await reader.calls(for: publication) == 2)
    await reader.release()
    _ = await old.value
    let retained = await service.resolvedScopes(list: snapshot, viewerDid: viewer)
    #expect(retained.first?.publicationSiteUrls == ["https://refreshed.example"])
    #expect(await reader.calls(for: publication) == 2)
  }

  private func list(_ key: String, publications: [String]) -> StandardReaderListDTO {
    .init(uri: "at://\(viewer)/app.standard-reader.list/\(key)", name: key, description: nil,
      creatorDid: viewer, publications: publications, users: [], owned: true, saved: false)
  }
}

private actor SharedPublicationReader: StandardReaderListReading {
  let blockedURI: String
  let blockFirstOnly: Bool
  private var counts: [String: Int] = [:]
  private var continuations: [CheckedContinuation<Void, Never>] = []
  private var waiters: [(Int, CheckedContinuation<Void, Never>)] = []
  private var released = false
  var totalCalls: Int { counts.values.reduce(0, +) }
  init(blockedURI: String, blockFirstOnly: Bool = false) { self.blockedURI = blockedURI; self.blockFirstOnly = blockFirstOnly }
  func calls(for uri: String) -> Int { counts[uri, default: 0] }
  func records(did: String, collection: String) async throws -> SemblePublicRecordRead { .init(records: [], complete: true) }
  func record(identity: StandardReaderListIdentity) async throws -> Data? { nil }
  func creatorDid(_ input: String) async throws -> String? { nil }
  func publicationDetails(_ uri: String) async throws -> StandardReaderListPublicationRead? {
    counts[uri, default: 0] += 1
    let count = counts[uri, default: 0]
    for (_, continuation) in waiters.filter({ $0.0 <= totalCalls }) { continuation.resume() }
    waiters.removeAll { $0.0 <= totalCalls }
    if uri == blockedURI && !released && (!blockFirstOnly || count == 1) {
      await withCheckedContinuation { continuations.append($0) }
    }
    let url = blockFirstOnly && count > 1 ? "https://refreshed.example" : "https://journal.example"
    return .init(details: .init(publicationId: uri, title: "Journal", authorDid: "did:plc:publisher",
      authorHandle: nil, iconUrl: nil, avatarUrl: nil), siteURL: url)
  }
  func waitForCalls(_ count: Int) async {
    if totalCalls >= count { return }
    await withCheckedContinuation { waiters.append((count, $0)) }
  }
  func release() {
    released = true
    let pending = continuations
    continuations.removeAll()
    for continuation in pending { continuation.resume() }
  }
}
