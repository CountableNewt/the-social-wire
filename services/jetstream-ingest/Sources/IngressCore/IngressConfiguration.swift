import Crypto
import Foundation

public struct IngressConfiguration: Sendable {
  public static let collections = [
    "app.thesocialwire.finance.selection", "app.thesocialwire.sports.selection",
    "site.standard.document", "site.standard.entry", "com.standard.document", "com.standard.entry",
    "app.skyreader.feed.subscription", "site.standard.graph.subscription",
  ]
  public let legacySingleLane: Bool
  public let environment: String
  public let databaseURL: String
  public let host: String
  public let generation: String
  public let collections: [String]
  public let apiKey: String
  public let port: Int
  public let batchSize: Int
  public let leaseName: String
  public let leaseTTL: Double
  public let trackedDIDRefresh: Double
  public let bootstrapAfter: Int64?
  public let incidentLimit: Int64
  public let dailyLimit: Int64
  public let backoffMin: Double
  public let backoffMax: Double
  public let poolMaxOpen: Int
  public let poolMaxIdle: Int
  public let poolIdleTimeout: Double
  public let maxDownloadAttempts: Int
  public let downloadConcurrency: Int
  public let segmentStripes: Int
  public let replayBudgetPause: Double
  public let streamNSID = "network.bsky.jetstream.subscribeEvents"
  public let cursorKind = "jetstream_v2_seq"
  public let scopePolicy = "publication-author-viewer-v1"
  public var fingerprint: String { Self.fingerprint(collections: collections) }

  public static func fingerprint(collections: [String]) -> String {
    let canonical = Set(collections.map { $0.trimmingCharacters(in: .whitespacesAndNewlines) })
      .sorted()
    return SHA256.hash(
      data: Data(
        ("network.bsky.jetstream.subscribeEvents\npublication-author-viewer-v1\n"
          + canonical.joined(separator: "\n")).utf8)
    ).map { String(format: "%02x", $0) }.joined()
  }

  public init(environment env: [String: String]) throws {
    func get(_ key: String, _ fallback: String = "") -> String {
      let value = env[key]?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
      return value.isEmpty ? fallback : value
    }
    func bool(_ key: String) throws -> Bool {
      switch get(key).lowercased() {
      case "", "false", "0", "f": return false
      case "true", "1", "t": return true
      default: throw IngressError.invalidConfiguration("Invalid boolean \(key)")
      }
    }
    let namespaced =
      env["JETSTREAM_APPVIEW_ENABLED"] != nil || env["JETSTREAM_WIRE_ENABLED"] != nil
      || env["JETSTREAM_WIRE_LANES"] != nil
    // Hosted environments retain selectors for disabled Wire lanes. As in Go,
    // those settings are inert until Wire is enabled.
    guard !(try bool("JETSTREAM_WIRE_ENABLED")),
      get("JETSTREAM_PIPELINE_MODE", "publication-author-viewer-v1")
        == "publication-author-viewer-v1"
    else {
      throw IngressError.invalidConfiguration(
        "The Swift ingress executable supports only the active AppView lane; Wire remains disabled")
    }
    if namespaced {
      guard try bool("JETSTREAM_APPVIEW_ENABLED") else {
        throw IngressError.invalidConfiguration("AppView lane must be enabled")
      }
    }
    legacySingleLane = !namespaced
    let prefix = namespaced ? "JETSTREAM_APPVIEW_" : "JETSTREAM_"
    func integer(_ key: String, _ fallback: Int) throws -> Int {
      guard let value = Int(get(key, String(fallback))) else {
        throw IngressError.invalidConfiguration("Invalid integer \(key)")
      }
      return value
    }
    func duration(_ key: String, _ fallback: String) throws -> Double {
      let value = get(key, fallback)
      let pattern = #"^([0-9]+(?:\.[0-9]+)?)(ms|s|m|h)$"#
      guard let regex = try? NSRegularExpression(pattern: pattern),
        let match = regex.firstMatch(in: value, range: NSRange(value.startIndex..., in: value)),
        let numberRange = Range(match.range(at: 1), in: value),
        let unitRange = Range(match.range(at: 2), in: value),
        let number = Double(value[numberRange])
      else { throw IngressError.invalidConfiguration("Invalid duration \(key)") }
      return number * ["ms": 0.001, "s": 1, "m": 60, "h": 3600][String(value[unitRange])]!
    }
    environment = get("APP_ENV")
    databaseURL = get("DATABASE_URL")
    host = get(prefix + "HOST", get("JETSTREAM_HOST", "jetstream.us-west.bsky.network"))
    generation = get(prefix + "SOURCE_GENERATION", "jetstream-v2-us-west-v2")
    let selected = get(prefix + "COLLECTIONS")
    collections =
      selected.isEmpty
      ? Self.collections
      : selected.split(separator: ",").map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
        .filter { !$0.isEmpty }
    apiKey = get(prefix + "API_KEY", get("JETSTREAM_API_KEY"))
    port = try integer("PORT", 8080)
    batchSize = try integer(prefix + "BATCH_SIZE", 256)
    leaseName = get(prefix + "LEADER_LEASE_NAME", "jetstream-v2-ingest")
    leaseTTL = try duration(prefix + "LEADER_LEASE_TTL", "30s")
    trackedDIDRefresh = try duration(prefix + "TRACKED_DID_REFRESH", "1m")
    if let raw = env[prefix + "BOOTSTRAP_AFTER_SEQ"], !raw.isEmpty {
      guard let seq = Int64(raw), seq >= 0 else {
        throw IngressError.invalidConfiguration("Invalid bootstrap cursor")
      }
      bootstrapAfter = seq
    } else {
      bootstrapAfter = nil
    }
    incidentLimit = Int64(try integer(prefix + "REPLAY_INCIDENT_BYTES", 5 << 30))
    dailyLimit = Int64(try integer(prefix + "REPLAY_DAILY_BYTES", 25 << 30))
    backoffMin = try duration(prefix + "BACKOFF_MIN", "250ms")
    backoffMax = try duration(prefix + "BACKOFF_MAX", "30s")
    poolMaxOpen = try integer(prefix + "DATABASE_POOL_MAX_OPEN", 8)
    poolMaxIdle = try integer(prefix + "DATABASE_POOL_MAX_IDLE", 1)
    poolIdleTimeout = try duration(prefix + "DATABASE_POOL_IDLE_TIMEOUT", "60s")
    maxDownloadAttempts = try integer(prefix + "MAX_DOWNLOAD_ATTEMPTS", 8)
    downloadConcurrency = try integer(prefix + "DOWNLOAD_CONCURRENCY", 4)
    segmentStripes = try integer(prefix + "SEGMENT_STRIPES", 4)
    replayBudgetPause = try duration(prefix + "REPLAY_BUDGET_PAUSE", "15m")
    guard ["dev", "prod"].contains(environment), !databaseURL.isEmpty, !apiKey.isEmpty,
      !apiKey.lowercased().hasPrefix("bearer "), !host.contains("/"), !host.contains("@"),
      !host.contains(":"), !generation.isEmpty, !collections.isEmpty,
      collections.allSatisfy({ (Self.collections + ["app.thesocialwire.readState"]).contains($0) }),
      port > 0, port <= 65535, batchSize > 0, batchSize <= 10000, leaseTTL >= 10,
      trackedDIDRefresh >= 5,
      incidentLimit > 0, dailyLimit >= incidentLimit, backoffMin > 0, backoffMax >= backoffMin,
      poolMaxOpen >= 2, poolMaxIdle >= 0, poolMaxIdle <= poolMaxOpen, poolIdleTimeout > 0,
      maxDownloadAttempts > 0, downloadConcurrency > 0, segmentStripes > 0, replayBudgetPause >= 60
    else { throw IngressError.invalidConfiguration("Invalid AppView ingress configuration") }
    guard get(prefix + "REPLAY_BEFORE_SEQ").isEmpty, !(try bool(prefix + "REPLAY_SNAPSHOT_ONLY")),
      !(try bool(prefix + "AUTOMATIC_RECOVERY_ENABLED")),
      get(prefix + "REPLAY_CANARY_EXPIRES_AT").isEmpty, !(try bool(prefix + "EXIT_AFTER_SNAPSHOT"))
    else {
      throw IngressError.invalidConfiguration(
        "Wire-only replay controls cannot be used by the AppView lane")
    }
  }
}
