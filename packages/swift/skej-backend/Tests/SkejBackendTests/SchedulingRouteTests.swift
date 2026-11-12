import Foundation
import Hummingbird
import HummingbirdTesting
import Logging
@testable import SkejBackend
import SkejKit
import Testing

private let selectedDID = "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa"
private let alternateDID = "did:plc:bbbbbbbbbbbbbbbbbbbbbbbb"
private let issuer = "https://pds.example.com"

@Test func callbackRejectsMismatchedActorsAndCannotBeReplayed() async throws {
    let store = try SQLiteStore(path: ":memory:")
    try await store.migrate()
    let pds = SQLitePDSClient(store: store)
    let config = AppConfig(publicOrigin: "https://api.example.com", network: .init())
    let logger = Logger(label: "scheduling-route-tests")
    let backend = EmbeddedSkejBackend(config: config, store: store, pdsClient: pds,
        oauthClient: MockSchedulingOAuth(), readLegacyQueue: nil,
        worker: ScheduleWorker(store: store, pdsClient: pds, logger: logger),
        submissionKeyBytes: Data(repeating: 9, count: 32), logger: logger)
    let router = Router(context: BasicRequestContext.self)
    backend.registerOAuth(on: router) { $0 == selectedDID }
    backend.registerScheduling(on: router.group()) { _ in selectedDID }

    let expiry = Timestamp.iso8601(Date().addingTimeInterval(600))
    for state in ["other-actor", "accepted"] {
        try await store.createOAuthState(state: state, handle: selectedDID, pkceVerifier: "verifier", nonce: "nonce",
            authServer: issuer, pdsEndpoint: issuer, initiatorDid: selectedDID, expiresAt: expiry)
    }

    try await Application(router: router).test(.router) { client in
        try await client.execute(uri: "/oauth/scheduling/start?accountDid=\(urlEncode(alternateDID))", method: .get) { response in
            #expect(response.status == .forbidden)
        }
        try await client.execute(uri: "/oauth/scheduling/callback?state=other-actor&code=alternate&iss=\(urlEncode(issuer))", method: .get) { response in
            #expect(response.status == .forbidden)
        }
        let callback = "/oauth/scheduling/callback?state=accepted&code=selected&iss=\(urlEncode(issuer))"
        try await client.execute(uri: callback, method: .get) { response in
            #expect(response.status == .ok)
            #expect(response.headers[.cacheControl] == "no-store")
        }
        try await client.execute(uri: callback, method: .get) { response in
            #expect(response.status == .badRequest)
        }
        try await client.execute(uri: "/xrpc/at.skej.schedule.list?accountDid=\(urlEncode(alternateDID))", method: .get) { response in
            #expect(response.status == .forbidden)
        }
    }
    #expect(try await store.oauthSession(did: alternateDID) == nil)
    #expect(try await store.oauthSession(did: selectedDID) != nil)
}

private struct MockSchedulingOAuth: OAuthClient {
    func start(handle: String, state: String, pkceVerifier: String, nonce: String) async throws -> OAuthStartResult {
        throw MockFailure.unexpected
    }

    func complete(state: OAuthStateRecord, code: String) async throws -> OAuthCompletion {
        let did = code == "alternate" ? alternateDID : selectedDID
        return OAuthCompletion(viewer: Viewer(did: did, handle: did, displayName: did, avatar: nil),
            session: OAuthSessionRecord(did: did, handle: did, tokenJSON: "test-token", dpopKeyJSON: "test-key"))
    }

    enum MockFailure: Error { case unexpected }
}
