import Foundation
import Testing

@testable import IngressCore

@Test func sourceFingerprintCanonicalizesCollections() {
  #expect(
    IngressConfiguration.fingerprint(collections: ["a", " b", "a"])
      == IngressConfiguration.fingerprint(collections: ["b", "a"]))
}
@Test func officialSDKEventsKeepEnvelopeAndIntegerPrecision() throws {
  let url = Bundle.module.url(
    forResource: "events", withExtension: "json", subdirectory: "Fixtures")!
  let payloads = try JSONDecoder().decode([JSONValue].self, from: Data(contentsOf: url))
  let events = try payloads.map(IngressEvent.init(payload:))
  #expect(events.map(\.sequence) == [101, 102, 103, 104])
  #expect(events.map(\.kind) == ["commit", "identity", "account", "sync"])
  #expect(events[0].payload["commit"]?["record_cbor"] == nil)
  #expect(events[3].revision == "3ksync")
}
@Test func postgresNormalizationPreservesLargeIntegersAndRejectsKeyCollision() throws {
  let value = try JSONDecoder().decode(
    JSONValue.self, from: Data(#"{"seq":9223372036854775807,"text":"a\u0000b"}"#.utf8))
  let normalized = try value.normalizedForPostgres()
  #expect(normalized["seq"] == .integer(Int64.max))
  #expect(normalized["text"] == .string("a\u{fffd}b"))
  #expect(throws: (any Error).self) {
    try JSONValue.object(["\0": .null, "\u{fffd}": .null]).normalizedForPostgres()
  }
}
@Test func schemaManifestRequiresEveryUniqueReceiptAndHostedDatabase() throws {
  #expect(
    try SchemaManifest.validate(["20261105000001", "20261104000001"]) == [
      "20261104000001", "20261105000001",
    ])
  #expect(throws: (any Error).self) {
    try SchemaManifest.validate(["20261105000001", "20261105000001"])
  }
  #expect(throws: (any Error).self) {
    try SchemaManifest.databaseURL(
      environment: ["APP_ENV": "local", "RAILWAY_SERVICE_ID": "service"], allowLocalSQLite: true)
  }
  #expect(
    try SchemaManifest.databaseURL(environment: ["APP_ENV": "local"], allowLocalSQLite: true) == nil
  )
}
@Test func configurationRefusesWireAndInvalidBootstrap() throws {
  let env = [
    "APP_ENV": "dev", "DATABASE_URL": "postgres://u:p@localhost/db", "JETSTREAM_API_KEY": "key",
  ]
  let config = try IngressConfiguration(environment: env)
  #expect(config.generation == "jetstream-v2-us-west-v2")
  #expect(config.poolMaxOpen == 8)
  #expect(throws: (any Error).self) {
    try IngressConfiguration(
      environment: env.merging(["JETSTREAM_WIRE_ENABLED": "true"]) { _, value in value })
  }
  #expect(throws: (any Error).self) {
    try IngressConfiguration(
      environment: env.merging(["JETSTREAM_BOOTSTRAP_AFTER_SEQ": "9223372036854775808"]) {
        _, value in value
      })
  }
}
@Test func liveFrameRejectsMalformedEnvelopeAndPreservesCommit() throws {
  let bytes = Data(
    #"{"$type":"message","payload":{"$type":"network.bsky.jetstream.subscribeEvents#commit","seq":42,"did":"did:plc:fixture","time":"2026-11-05T01:02:03.123456Z","rev":"rev","operation":"create","collection":"site.standard.document","rkey":"key","record":{"$type":"site.standard.document","title":"Hello"}}}"#
      .utf8)
  let event = try #require(try IngressEvent.decodeLive(bytes))
  #expect(event.sequence == 42)
  #expect(event.collection == "site.standard.document")
  #expect(event.timeUS == 1_793_840_523_123_456)
  #expect(throws: (any Error).self) {
    try IngressEvent.decodeLive(Data(#"{"did":"did:plc:x"}"#.utf8))
  }
}
@Test func dagCBORHandlesSentinelsAndRejectsTruncatedOrTrailingValues() throws {
  #expect(try DAGCBOR.decode(Data([0xa1, 0x61, 0x61, 0x01])) == .object(["a": .integer(1)]))
  #expect(try DAGCBOR.decode(Data([0x42, 0x01, 0x02])) == .object(["$bytes": .string("AQI")]))
  #expect(throws: (any Error).self) { try DAGCBOR.decode(Data([0xa1, 0x61])) }
  #expect(throws: (any Error).self) { try DAGCBOR.decode(Data([0xa0, 0])) }
  #expect(DAGCBOR.cid(for: Data([0xa0])).hasPrefix("bafyre"))
}
@Test func archiveRejectsImpossibleCountsAndTrailingData() throws {
  #expect(throws: (any Error).self) { try ArchiveBlockDecoder.decode(Data([255, 255, 255, 255])) }
  #expect(try ArchiveBlockDecoder.decode(Data([0, 0, 0, 0])).isEmpty)
  #expect(throws: (any Error).self) { try ArchiveBlockDecoder.decode(Data([0, 0, 0, 0, 1])) }
}
@Test func downloadRangeAndRetryAfterAreValidated() {
  #expect(ArchiveTransport.contentRange("bytes 10-19/20")?.start == 10)
  #expect(ArchiveTransport.contentRange("bytes 10-21/20") == nil)
  #expect(ArchiveTransport.retryDelay("15", fallback: 1) == 15)
  #expect(ArchiveTransport.retryDelay("-5", fallback: 1) == 1)
}
@Test func replayBudgetCannotResetIncidentOnRestartAndCountsFailedBytes() async throws {
  let budget = ReplayBudget(incidentLimit: 10, dailyLimit: 20, incident: 9)
  try await budget.add(1)
  await #expect(throws: (any Error).self) { try await budget.check() }
  await #expect(throws: (any Error).self) { try await budget.add(1) }
  #expect(await budget.incident == 11)
}

@Test func providerRejectionDoesNotTranslateFutureCursor() {
  let future = JetstreamUpgradeHandler.rejection(
    status: 400, body: Data(#"{"error":"FutureCursor","message":"provider epoch mismatch"}"#.utf8))
  if case .sourceDiscontinuity = future {
  } else {
    Issue.record("FutureCursor must block the source")
  }
  let old = JetstreamUpgradeHandler.rejection(
    status: 400, body: Data(#"{"error":"CursorTooOld"}"#.utf8))
  if case .cursorTooOld = old {} else { Issue.record("CursorTooOld must request archive recovery") }
}
@Test func liveTimestampPreservesMicrosecondsAcrossOffsetsAndNegativeEpoch() {
  #expect(IngressEvent.parseTimeUS("2026-11-05T01:02:03.123456Z") == 1_793_840_523_123_456)
  #expect(IngressEvent.parseTimeUS("2026-11-04T19:02:03.123456-06:00") == 1_793_840_523_123_456)
  #expect(IngressEvent.parseTimeUS("1969-12-31T23:59:59.999999Z") == -1)
}

@Test func liveRecordSentinelsAreValidatedBeforeStaging() throws {
  #expect(
    try ATProtoRecordJSON.normalize(.object(["$bytes": .string("AQI=")]))
      == .object(["$bytes": .string("AQI")]))
  #expect(throws: (any Error).self) {
    try ATProtoRecordJSON.normalize(.object(["$bytes": .string("!!")]))
  }
  #expect(throws: (any Error).self) {
    try ATProtoRecordJSON.normalize(.object(["$link": .string("bafy-invalid")]))
  }
  #expect(ATProtoRecordJSON.validCID(DAGCBOR.cid(for: Data([0xa0]))))
}
@Test func controllerHealthRetainsLaneAndReadinessContract() async throws {
  let health = IngressHealth()
  #expect(await health.document(controller: true)["ready"] == .bool(false))
  await health.database(true)
  #expect(await health.document(controller: true)["started"] == .bool(true))
  await health.lease(true)
  await health.stream(true)
  #expect(await health.document(controller: true)["lanes"]?["appview"]?["ready"] == .bool(true))
  await health.paused(true)
  #expect(await health.document(controller: false)["ready"] == .bool(false))
}
