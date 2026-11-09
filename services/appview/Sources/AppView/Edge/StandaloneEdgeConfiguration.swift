import Foundation
import GatewayCore

/// Public ingress settings for the single-process reader backend. Environment
/// identity stays dev/prod even when persistence is SQLite.
struct StandaloneEdgeConfiguration: Sendable {
  let sqlitePath: String
  let attestationReceipt: ATProtoSessionAttestationReceipt
  let latrProxy: LatrIosProxyCredentials.Config?
  let entitlements: GatewayEntitlements
  let mediaWorkerURL: String?
  let klipyAPIKey: String

  static func load(_ environment: [String: String], appEnvironment: GatewayConfig.AppEnvironment)
    throws -> Self?
  {
    let mode =
      environment["BACKEND_MODE"]?.trimmingCharacters(in: .whitespacesAndNewlines) ?? "distributed"
    guard mode == "distributed" || mode == "standalone" else { throw Failure.invalidMode }
    guard mode == "standalone" else { return nil }
    let path = environment["SQLITE_DB_PATH"]?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
    guard !path.isEmpty, appEnvironment == .local || path.hasPrefix("/") else {
      throw Failure.missingPersistentSQLitePath
    }
    let secret =
      environment["PDS_ATTESTATION_RECEIPT_SECRET"]?.trimmingCharacters(in: .whitespacesAndNewlines)
      ?? ""
    guard !secret.isEmpty else { throw Failure.missingAttestationSecret }
    let receipt: ATProtoSessionAttestationReceipt
    do { receipt = try ATProtoSessionAttestationReceipt(secret: secret) } catch {
      throw Failure.invalidAttestationSecret
    }
    return Self(
      sqlitePath: path,
      attestationReceipt: receipt,
      latrProxy: LatrIosProxyCredentials.Config.fromEnvironment(environment),
      entitlements: GatewayEntitlements(
        gatingEnabled: environment["SOCIALWIRE_PRO_GATING_ENABLED"]?.lowercased() == "true",
        proDIDs: Set(
          (environment["SOCIALWIRE_PRO_DIDS"] ?? "").split { $0 == "," || $0.isWhitespace }.map(
            String.init))
      ),
      mediaWorkerURL: environment["PODCAST_MEDIA_WORKER_URL"]?.trimmingCharacters(
        in: .whitespacesAndNewlines),
      klipyAPIKey: [environment["KLIPY_API_KEY"], environment["KLIPPY_API_KEY"]].compactMap {
        $0?.trimmingCharacters(in: .whitespacesAndNewlines)
      }.first { !$0.isEmpty } ?? ""
    )
  }

  enum Failure: Error, Equatable {
    case invalidMode
    case missingPersistentSQLitePath
    case missingAttestationSecret
    case invalidAttestationSecret
    case retiredIngestionEnabled
  }
}
