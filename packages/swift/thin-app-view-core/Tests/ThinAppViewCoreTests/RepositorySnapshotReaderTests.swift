import AsyncHTTPClient
import Foundation
@preconcurrency import GRDB
import Logging
import NIOCore
import Testing

@testable import ThinAppViewCore

@Suite("Verified repository snapshots")
struct RepositorySnapshotReaderTests {
  // Generated with the independent github.com/jcalabro/atmos v0.3.7 repository
  // writer. Each fixture has 63 document records, an unrelated post, a signed
  // commit, and canonical MST blocks; no private signing keys are retained.
  @Test(arguments: ["p256", "k256"])
  func verifiesIndependentSignedFixtures(curve: String) async throws {
    let fixture = try Fixture(curve)
    let transport = FixtureTransport(fixture)
    let snapshot = try await RepositorySnapshotReader(transport: transport).read(
      did: fixture.did,
      collections: ["site.standard.document"])
    #expect(snapshot.commitCID == fixture.cid.string)
    #expect(snapshot.records.count == 63)
    #expect(snapshot.records.first?.uri == "at://\(fixture.did)/site.standard.document/key000")
    #expect(snapshot.records.allSatisfy { $0.uri.contains("/site.standard.document/") })
    let value = try JSONSerialization.jsonObject(with: snapshot.records[0].value) as? [String: Any]
    #expect(value?["title"] as? String == "Record 0")
    #expect(value?["bytes"] as? [String: String] == ["$bytes": "AQI"])
    let blob = value?["blob"] as? [String: Any]
    let reference = blob?["ref"] as? [String: String]
    #expect(reference?["$link"]?.hasPrefix("bafk") == true)
    #expect(await transport.count("/xrpc/com.atproto.sync.getLatestCommit") == 2)
  }

  @Test(arguments: [
    Mutation.inactive, .inactiveAtEnd, .numericActive, .identityChanged, .signingKeyChanged,
    .commitChanged, .missingBlock, .corruptBlock,
    .unrequestedBlock,
  ])
  func rejectsUnsafeOrIncompleteSnapshots(mutation: Mutation) async throws {
    let fixture = try Fixture("p256")
    let transport = FixtureTransport(fixture, mutation: mutation)
    await #expect(throws: (any Error).self) {
      try await RepositorySnapshotReader(transport: transport).read(
        did: fixture.did, collections: ["site.standard.document"])
    }
  }

  @Test(arguments: ["p256", "k256"])
  func rejectsCorruptSignaturesEvenWithValidBlockHash(curve: String) throws {
    let fixture = try Fixture(curve)
    let raw = try #require(fixture.blocks[fixture.cid])
    guard case .map(var commit) = try RepositoryDAGCBOR.decode(raw),
      var signature = commit["sig"]?.data
    else {
      Issue.record("Missing fixture signature")
      return
    }
    signature[0] ^= 1
    commit["sig"] = .bytes(signature)
    let corrupt = RepositoryDAGCBOR.encode(.map(commit))
    #expect(throws: (any Error).self) {
      try RepositorySnapshotValidator.commit(
        raw: corrupt, cid: .hash(corrupt), did: fixture.did,
        revision: fixture.revision, key: RepositorySigningKey(multibase: fixture.key), now: Date())
    }
  }

  @Test func preservesSignedCheckpointIdentity() throws {
    let raw = Data(
      #"{"snapshotId":"snapshot","pruningComplete":false,"startedAt":1,"collections":{},"completed":false,"snapshotMode":"signed_blocks_v1","commitCid":"commit","revision":"rev","recordOffset":50}"#
        .utf8)
    let state = try JSONDecoder().decode(PDSRepositoryRecoveryState.self, from: raw)
    #expect(state.snapshotMode == "signed_blocks_v1")
    let next = try JSONDecoder().decode(
      PDSRepositoryRecoveryState.self, from: JSONEncoder().encode(state))
    #expect(next.commitCid == "commit")
    #expect(next.revision == "rev")
    #expect(next.recordOffset == 50)
  }

  @Test(arguments: ["p256", "k256"])
  func rejectsHighSSignatures(curve: String) throws {
    let fixture = try Fixture(curve)
    guard
      case .map(var commit) = try RepositoryDAGCBOR.decode(#require(fixture.blocks[fixture.cid])),
      let signature = commit["sig"]?.data
    else {
      Issue.record("Missing fixture commit")
      return
    }
    let p256Order: [UInt8] = [
      0xff, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
      0xff,
      0xbc, 0xe6, 0xfa, 0xad, 0xa7, 0x17, 0x9e, 0x84, 0xf3, 0xb9, 0xca, 0xc2, 0xfc, 0x63, 0x25,
      0x51,
    ]
    let k256Order: [UInt8] = [
      0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
      0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xfe,
      0xba, 0xae, 0xdc, 0xe6, 0xaf, 0x48, 0xa0, 0x3b,
      0xbf, 0xd2, 0x5e, 0x8c, 0xd0, 0x36, 0x41, 0x41,
    ]
    let order = curve == "p256" ? p256Order : k256Order
    let s = Array(signature.suffix(32))
    var highS = [UInt8](repeating: 0, count: 32)
    var borrow = 0
    for index in stride(from: 31, through: 0, by: -1) {
      var value = Int(order[index]) - Int(s[index]) - borrow
      borrow = value < 0 ? 1 : 0
      if value < 0 { value += 256 }
      highS[index] = UInt8(value)
    }
    commit["sig"] = .bytes(Data(signature.prefix(32)) + Data(highS))
    let raw = RepositoryDAGCBOR.encode(.map(commit))
    #expect(
      throws: RepositorySnapshotError.invalid(
        curve == "p256" ? "high-S signature" : "invalid commit signature")
    ) {
      try RepositorySnapshotValidator.commit(
        raw: raw, cid: .hash(raw), did: fixture.did,
        revision: fixture.revision, key: RepositorySigningKey(multibase: fixture.key), now: Date())
    }
  }

  @Test func rejectsSharedMSTChildrenAndWholeSetPrivateDNS() throws {
    let fixture = try Fixture("p256")
    let commit = try RepositoryDAGCBOR.decode(#require(fixture.blocks[fixture.cid]))
    let root = try #require(commit["data"]?.cid)
    guard case .map(var node) = try RepositoryDAGCBOR.decode(#require(fixture.blocks[root])),
      var entries = node["e"]?.values, !entries.isEmpty, case .map(var first) = entries[0]
    else {
      Issue.record("Missing fixture MST")
      return
    }
    let child = try #require(node["l"]?.cid ?? first["t"]?.cid)
    node["l"] = .link(child)
    first["t"] = .link(child)
    entries[0] = .map(first)
    node["e"] = .array(entries)
    let raw = RepositoryDAGCBOR.encode(.map(node))
    let alteredRoot = RepositoryCID.hash(raw)
    var blocks = fixture.blocks
    blocks[alteredRoot] = raw
    var tree = RepositoryMST()
    #expect(throws: (any Error).self) { try tree.walk(root: alteredRoot, blocks: blocks) }
    #expect(RepositoryDNSAddressValidator.preferredPublicAddress(["8.8.8.8", "127.0.0.1"]) == nil)
    #expect(RepositoryDNSAddressValidator.preferredPublicAddress(["8.8.8.8", "10.0.0.1"]) == nil)
    #expect(
      RepositoryDNSAddressValidator.preferredPublicAddress(["8.8.8.8", "::ffff:127.0.0.1"]) == nil)
    #expect(
      RepositoryDNSAddressValidator.preferredPublicAddress(["2606:4700:4700::1111", "8.8.8.8"])
        == "8.8.8.8")
  }

  @Test(arguments: ["p256", "p256-large", "p256-list-fallback"])
  func restoresSignedSnapshotWithFencedCheckpointAndIdempotentCompletion(curve: String) async throws
  {
    let fixture = try Fixture(curve == "p256-list-fallback" ? "p256" : curve)
    let expectedRecords = curve == "p256-large" ? 250 : 63
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer { try? FileManager.default.removeItem(atPath: path) }
    let logger = Logger(label: "signed-recovery")
    let token = UUID().uuidString
    let store = try SQLiteThinAppViewStore(path: path, logger: logger)
    var context = PDSRepositoryRecoveryContext(
      environment: "test", sourceGeneration: "generation", sequence: 1,
      repoDid: fixture.did, requestId: nil, workerId: "worker", leaseToken: token)
    try await store.db.write { db in
      try db.execute(
        sql: """
          INSERT INTO appview_ingestion_inbox
            (environment,source_generation,seq,source_host,cursor_kind,event_kind,repo_did,payload,event_time,
             status,next_attempt_at,staged_at,updated_at,lease_owner,lease_token,lease_expires_at)
          VALUES ('test','generation',1,'test','jetstream_v2_seq','sync',?,'{}','2026-01-01T00:00:00Z',
            'leased','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','worker',?,'2099-01-01T00:00:00Z')
          """, arguments: [fixture.did, token])
    }
    var state = try await store.loadRepositoryRecovery(context)
    if curve != "p256-list-fallback" { state.snapshotMode = "signed_blocks_v1" }
    try await store.saveRepositoryRecovery(context, state: state, observedURIs: [], finish: false)
    let config = ThinAppViewConfig.fromEnvironment([
      "ENABLE_THIN_APPVIEW": "true", "THIN_APPVIEW_JETSTREAM_MODE": "v2_authoritative",
    ])
    let indexer = ThinAppViewIndexer(store: store, config: config, logger: logger)
    let invalidPDS = InvalidDocumentPDS(document: fixture.document)
    let pdsTransport: any PDSHTTPTransport =
      curve == "p256-list-fallback" ? invalidPDS : UnreachablePDS()
    let backfill = ThinAppViewEnrollBackfill(
      store: store, indexer: indexer, httpTransport: pdsTransport,
      endpointPolicy: .publicHTTPS, plcURL: "https://plc.directory", config: config, logger: logger)
    var restorer = TapPDSRepositoryRestorer(
      store: store, backfill: backfill, maxConcurrency: 1, rateLimitPerSecond: 10)
    let transport = FixtureTransport(fixture)
    restorer.snapshotReader = RepositorySnapshotReader(transport: transport)
    var firstSnapshotID: String?
    if expectedRecords > 200 {
      await #expect(throws: PDSRepositoryRecoveryError.yielded) {
        try await restorer.restoreCurrentRepository(repoDid: fixture.did, recovery: context)
      }
      let checkpoint = try await store.db.read { db in
        try String.fetchOne(db, sql: "SELECT recovery_state FROM appview_ingestion_inbox")!
      }
      let saved = try JSONDecoder().decode(
        PDSRepositoryRecoveryState.self, from: Data(checkpoint.utf8))
      #expect(saved.recordOffset == 200)
      #expect(!saved.completed)
      firstSnapshotID = saved.snapshotId
      #expect(
        try await store.db.read { try Int.fetchOne($0, sql: "SELECT COUNT(*) FROM content_items") }
          == 200)
      let expired = context
      let nextToken = UUID().uuidString
      try await store.db.write { db in
        try db.execute(
          sql:
            "UPDATE appview_ingestion_inbox SET status='leased',lease_owner='worker',lease_token=?,lease_expires_at='2099-01-01T00:00:00Z'",
          arguments: [nextToken])
      }
      context = .init(
        environment: "test", sourceGeneration: "generation", sequence: 1,
        repoDid: fixture.did, requestId: nil, workerId: "worker", leaseToken: nextToken)
      await #expect(throws: AppViewIngestionInboxStoreError.staleLease) {
        try await store.loadRepositoryRecovery(expired)
      }
    }
    let report = try await restorer.restoreCurrentRepository(
      repoDid: fixture.did, recovery: context)
    #expect(report.complete)
    #expect(report.indexedCount == expectedRecords)
    #expect(
      try await store.db.read { try Int.fetchOne($0, sql: "SELECT COUNT(*) FROM content_items") }
        == expectedRecords)
    let saved = try await store.loadRepositoryRecovery(context)
    #expect(saved.completed)
    #expect(saved.snapshotMode == "signed_blocks_v1")
    #expect(saved.commitCid == fixture.cid.string)
    #expect(saved.recordOffset == expectedRecords)
    if let firstSnapshotID { #expect(saved.snapshotId == firstSnapshotID) }
    let calls = await transport.count("/xrpc/com.atproto.sync.getLatestCommit")
    #expect(
      try await restorer.restoreCurrentRepository(repoDid: fixture.did, recovery: context).complete)
    #expect(await transport.count("/xrpc/com.atproto.sync.getLatestCommit") == calls)
    if curve == "p256-list-fallback" { #expect(await invalidPDS.documentLimits() == [10, 1]) }
  }

  @Test func boundsWholeAttemptIncludingRetries() async throws {
    let fixture = try Fixture("p256")
    let rateLimited = FixtureTransport(fixture, mutation: .rateLimited)
    await #expect(throws: (any Error).self) {
      try await RepositorySnapshotReader(transport: rateLimited).read(
        did: fixture.did, collections: ["site.standard.document"])
    }
    #expect(await rateLimited.count("/" + fixture.did) == 3)
    var byteLimited = RepositorySnapshotReader(transport: FixtureTransport(fixture))
    byteLimited.maximumBytes = 8
    await #expect(throws: (any Error).self) {
      try await byteLimited.read(did: fixture.did, collections: ["site.standard.document"])
    }
    var requestLimited = RepositorySnapshotReader(transport: FixtureTransport(fixture))
    requestLimited.maximumRequests = 1
    await #expect(throws: (any Error).self) {
      try await requestLimited.read(did: fixture.did, collections: ["site.standard.document"])
    }
  }

  @Test func cancelsAnUnresponsiveTransport() async throws {
    let fixture = try Fixture("p256")
    let transport = FixtureTransport(fixture, mutation: .stall)
    var reader = RepositorySnapshotReader(transport: transport)
    reader.timeout = .milliseconds(5)
    await #expect(throws: (any Error).self) {
      try await reader.read(did: fixture.did, collections: ["site.standard.document"])
    }
    #expect(await transport.activeRequestCount() == 0)
    #expect(await transport.cancelledRequestCount() == 1)
  }

  @Test func preservesValidNumericNSIDDomainLabels() {
    #expect(RepositoryMST.validPath("com.123.record/key"))
    #expect(RepositoryMST.validPath("com.a-123.record/key"))
    #expect(!RepositoryMST.validPath("123.example.record/key"))
    #expect(!RepositoryMST.validPath("com.example.123record/key"))
    #expect(!RepositoryMST.validPath("com.-123.record/key"))
    #expect(!RepositoryMST.validPath("com.123-.record/key"))
  }

  @Test func rejectsNoncanonicalCBORAndCIDEncoding() {
    #expect(throws: (any Error).self) { try RepositoryDAGCBOR.decode(Data([0x18, 0x01])) }
    #expect(throws: (any Error).self) {
      try RepositoryDAGCBOR.decode(Data([0xa2, 0x61, 0x62, 0x01, 0x61, 0x61, 0x02]))
    }
    #expect(throws: (any Error).self) {
      try RepositoryDAGCBOR.decode(Data([0xa2, 0x61, 0x61, 0x01, 0x61, 0x61, 0x02]))
    }
    #expect(throws: (any Error).self) { try RepositoryCID("b" + String(repeating: "a", count: 58)) }
    #expect(throws: (any Error).self) {
      try RepositorySnapshotValidator.validateRevision("zzzzzzzzzzzzz", now: Date())
    }
  }

  private struct Fixture: Sendable {
    let did: String, pds: String, key: String, revision: String
    let cid: RepositoryCID
    let document: Data
    let blocks: [RepositoryCID: Data]

    init(_ curve: String) throws {
      let path = try #require(
        Bundle.module.url(
          forResource: curve, withExtension: "json", subdirectory: "Fixtures/RepositorySnapshots"))
      let value = try #require(
        JSONSerialization.jsonObject(with: Data(contentsOf: path)) as? [String: Any])
      did = try #require(value["did"] as? String)
      pds = try #require(value["pds"] as? String)
      key = try #require(value["key"] as? String)
      revision = try #require(value["revision"] as? String)
      cid = try RepositoryCID(#require(value["cid"] as? String))
      document = try JSONSerialization.data(withJSONObject: #require(value["document"]))
      let source = try #require(value["blocks"] as? [String: String])
      blocks = try Dictionary(
        uniqueKeysWithValues: source.map {
          (try RepositoryCID($0.key), try #require(Data(base64Encoded: $0.value)))
        })
    }
  }

  enum Mutation: Sendable {
    case inactive, inactiveAtEnd, numericActive, identityChanged, signingKeyChanged, commitChanged,
      missingBlock, corruptBlock, unrequestedBlock,
      rateLimited, stall
  }

  private struct UnreachablePDS: PDSHTTPTransport {
    func execute(_ request: HTTPClientRequest, timeout: TimeAmount) async throws
      -> HTTPClientResponse
    {
      throw RepositorySnapshotError.invalid(
        "listRecords must not replace signed snapshot authority")
    }
  }

  private actor InvalidDocumentPDS: PDSHTTPTransport {
    let document: Data
    var limits: [Int] = []
    init(document: Data) { self.document = document }
    func documentLimits() -> [Int] { limits }
    func execute(_ request: HTTPClientRequest, timeout: TimeAmount) async throws
      -> HTTPClientResponse
    {
      let url = try #require(URL(string: request.url))
      if url.path.hasPrefix("/did:") {
        return .init(status: .ok, body: .bytes(ByteBuffer(bytes: document)))
      }
      if url.path == "/xrpc/com.atproto.repo.listRecords" {
        let query = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        if query.first(where: { $0.name == "collection" })?.value == "site.standard.document" {
          limits.append(Int(query.first(where: { $0.name == "limit" })?.value ?? "") ?? -1)
          return .init(
            status: .badRequest,
            body: .bytes(
              ByteBuffer(string: #"{"error":"InvalidRequest","message":"serialization failed"}"#)))
        }
        return .init(status: .ok, body: .bytes(ByteBuffer(string: #"{"records":[]}"#)))
      }
      throw RepositorySnapshotError.invalid("unexpected listRecords transport request")
    }
  }

  private actor FixtureTransport: RepositorySnapshotHTTPTransport {
    let fixture: Fixture
    let mutation: Mutation?
    var calls: [String: Int] = [:]
    var activeRequests = 0, cancelledRequests = 0
    init(_ fixture: Fixture, mutation: Mutation? = nil) {
      self.fixture = fixture
      self.mutation = mutation
    }
    func count(_ path: String) -> Int { calls[path, default: 0] }
    func activeRequestCount() -> Int { activeRequests }
    func cancelledRequestCount() -> Int { cancelledRequests }

    func get(url: URL, accept: String, maximumBytes: Int, deadline: ContinuousClock.Instant)
      async throws -> RepositorySnapshotHTTPResponse
    {
      activeRequests += 1
      defer {
        activeRequests -= 1
        if Task.isCancelled { cancelledRequests += 1 }
      }
      calls[url.path, default: 0] += 1
      if mutation == .stall { try await Task.sleep(for: .seconds(3_600)) }
      if mutation == .rateLimited { return .init(status: 429, retryAfter: "0", body: Data()) }
      let body: Data
      switch url.path {
      case "/" + fixture.did:
        if mutation == .identityChanged && calls[url.path] == 2 {
          var document = try #require(
            JSONSerialization.jsonObject(with: fixture.document) as? [String: Any])
          document["service"] = [
            [
              "id": "#atproto_pds", "type": "AtprotoPersonalDataServer",
              "serviceEndpoint": "https://moved.publisher.social",
            ]
          ]
          body = try JSONSerialization.data(withJSONObject: document)
        } else if mutation == .signingKeyChanged && calls[url.path] == 2 {
          var document = try #require(
            JSONSerialization.jsonObject(with: fixture.document) as? [String: Any])
          var methods = try #require(document["verificationMethod"] as? [[String: Any]])
          methods[0]["publicKeyMultibase"] = try Fixture("k256").key
          document["verificationMethod"] = methods
          body = try JSONSerialization.data(withJSONObject: document)
        } else {
          body = fixture.document
        }
      case "/xrpc/com.atproto.sync.getRepoStatus":
        body = try JSONSerialization.data(withJSONObject: [
          "did": fixture.did,
          "active": mutation == .numericActive
            ? 1
            : (mutation != .inactive && !(mutation == .inactiveAtEnd && calls[url.path] == 2))
              as Any,
        ])
      case "/xrpc/com.atproto.sync.getLatestCommit":
        let revision =
          mutation == .commitChanged && calls[url.path] == 2
          ? String(fixture.revision.dropLast()) + (fixture.revision.last == "2" ? "3" : "2")
          : fixture.revision
        body = try JSONSerialization.data(withJSONObject: [
          "cid": fixture.cid.string, "rev": revision,
        ])
      case "/xrpc/com.atproto.sync.getBlocks":
        let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        var requested = try items.filter { $0.name == "cids" }.map {
          try RepositoryCID(#require($0.value))
        }
        if mutation == .missingBlock { requested.removeLast() }
        if mutation == .unrequestedBlock,
          let extra = fixture.blocks.keys.first(where: { !requested.contains($0) })
        {
          requested.append(extra)
        }
        var car = Self.section(
          RepositoryDAGCBOR.encode(.map(["version": .unsigned(1), "roots": .array([])])))
        for cid in requested {
          var bytes = try #require(fixture.blocks[cid])
          if mutation == .corruptBlock { bytes[0] ^= 1 }
          car += Self.section(cid.bytes + bytes)
        }
        body = car
      default: throw RepositorySnapshotError.invalid("unexpected fixture request")
      }
      return .init(status: 200, retryAfter: nil, body: body)
    }

    private static func section(_ bytes: Data) -> Data {
      var length = bytes.count
      var prefix = Data()
      repeat {
        let next = length & 127
        length >>= 7
        prefix.append(UInt8(next | (length == 0 ? 0 : 128)))
      } while length > 0
      return prefix + bytes
    }
  }
}
