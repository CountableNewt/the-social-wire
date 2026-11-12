import Crypto
import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import Logging
import SkejKit

public struct EmbeddedSkejBackend: Sendable {
    let config: AppConfig
    let store: SQLiteStore
    let pdsClient: any PDSClient
    let oauthClient: any OAuthClient
    let readLegacyQueue: (@Sendable (String) async throws -> [String: SkejScheduleRecord])?
    let worker: ScheduleWorker
    let submissionKeyBytes: Data
    let logger: Logger
    let createAdmission = ScheduleCreateAdmission()

    public static func open(
        publicOrigin: String,
        sqlitePath: String,
        authEncryptionSecret: String,
        importLegacySchedules: Bool,
        logger: Logger,
        transport: @escaping @Sendable (URLRequest) async throws -> (Data, HTTPURLResponse)
    ) async throws -> EmbeddedSkejBackend {
        guard let origin = SkejATProtoNetwork.origin(publicOrigin),
              sqlitePath.hasPrefix("/"), !authEncryptionSecret.isEmpty else {
            throw ConfigurationError.invalidConfiguration
        }
        let config = AppConfig(publicOrigin: origin, network: .init())
        let store = try SQLiteStore(path: sqlitePath, authEncryptionSecret: authEncryptionSecret, requiresAuthEncryption: true)
        try await store.migrate()
        let http = SchedulingHTTPClient(transport: transport)
        let pds = ATProtoPDSClient(store: store, clientID: config.clientID, http: http)
        let queueReader: (@Sendable (String) async throws -> [String: SkejScheduleRecord])?
        if importLegacySchedules {
            queueReader = { did in try await pds.listAuthoritativeSchedules(did: did) }
        } else {
            queueReader = nil
        }
        let linkPreview = LinkPreviewService(pdsClient: pds, http: EmbeddedLinkPreviewHTTPClient(transport: transport), logger: logger)
        return EmbeddedSkejBackend(
            config: config, store: store, pdsClient: pds, oauthClient: ATProtoOAuthClient(config: config, http: http),
            readLegacyQueue: queueReader,
            worker: ScheduleWorker(store: store, pdsClient: pds, logger: logger, linkPreviewHydrator: linkPreview,
                allowSchedulesWithoutCalendar: true),
            submissionKeyBytes: Data(SHA256.hash(data: Data(("schedule-submissions:" + authEncryptionSecret).utf8))),
            logger: logger
        )
    }

    public func runTick() async {
        await recoverPendingSubmission()
        await worker.runTick()
    }

    enum ConfigurationError: Error {
        case invalidConfiguration
    }
}

actor ScheduleCreateAdmission {
    private var activeAccounts = Set<String>()

    func acquire(_ did: String) -> Bool {
        activeAccounts.insert(did).inserted
    }

    func release(_ did: String) {
        activeAccounts.remove(did)
    }
}
