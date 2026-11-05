import Testing

@testable import IngressCore

@Test(arguments: ["publicationwest", "external,publication"])
func configurationIgnoresDisabledWireLaneSettings(selector: String) throws {
  let active = [
    "APP_ENV": "dev", "DATABASE_URL": "postgres://u:p@localhost/db", "JETSTREAM_API_KEY": "key",
    "JETSTREAM_APPVIEW_ENABLED": "true", "JETSTREAM_APPVIEW_SOURCE_GENERATION": "appview-fixture",
  ]
  let expected = try IngressConfiguration(environment: active)
  let dormant = [
    "JETSTREAM_WIRE_LANES": selector,
    "JETSTREAM_WIRE_REPLAY_SNAPSHOT_ONLY": "true",
    "JETSTREAM_WIRE_PUBLICATIONWEST_REPLAY_SNAPSHOT_ONLY": "true",
    "JETSTREAM_WIRE_EXTERNAL_BOOTSTRAP_AFTER_SEQ": "invalid-dormant-value",
  ]
  for enabled in [nil, "false"] as [String?] {
    var environment = active.merging(dormant) { _, value in value }
    environment["JETSTREAM_WIRE_ENABLED"] = enabled
    let actual = try IngressConfiguration(environment: environment)
    #expect(actual.generation == expected.generation)
    #expect(actual.collections == expected.collections)
    #expect(actual.fingerprint == expected.fingerprint)
    #expect(actual.legacySingleLane == false)
    environment["JETSTREAM_WIRE_ENABLED"] = "true"
    #expect(throws: (any Error).self) { try IngressConfiguration(environment: environment) }
  }
}
