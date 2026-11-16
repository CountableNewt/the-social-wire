import Foundation
import Testing
@testable import SocialWire

@Suite("Saved-link session ownership")
@MainActor
struct SavedLinkSessionOwnershipTests {
    private func fixture() async throws -> (SocialWireAppModel, ATProtoOAuthService, OAuthTestSessionStore, AuthSession, SavedLinkPreviewTransport) {
        let store = OAuthTestSessionStore()
        let auth = ATProtoOAuthService(keychain: store)
        let session = AuthSession(did: "did:plc:saved-\(UUID().uuidString)", pdsURL: URL(string: "https://pds.example.test")!,
            tokenEndpoint: URL(string: "https://issuer.example/token")!, accessToken: "access", refreshToken: "refresh",
            tokenType: "DPoP", scope: ATProtoOAuthService.scopes, expiresAt: .distantFuture)
        try await restore(session, auth: auth, store: store)
        let transport = SavedLinkPreviewTransport()
        let model = SocialWireAppModel(authService: auth, gatewayTransport: { try await transport.send($0) },
                                       initialReaderListSource: .subscribed)
        return (model, auth, store, session, transport)
    }

    private func restore(_ session: AuthSession, auth: ATProtoOAuthService, store: OAuthTestSessionStore) async throws {
        let record: [String: Any] = [
            "session": try JSONSerialization.jsonObject(with: JSONEncoder().encode(session)),
            "dpopPrivateKey": Data(repeating: 1, count: 32).base64EncodedString(), "clientID": ATProtoOAuthConfig.clientID
        ]
        store.set(try JSONSerialization.data(withJSONObject: record).base64EncodedString(), for: "oauth.session.v2")
        await auth.restoreSession()
    }

    private func save(viewerDID: String) -> MergedLatrSave {
        .native(MergedLatrNativeSave(savedAt: "2026-11-05T00:00:00Z", itemRkey: "saved",
            itemUri: "at://\(viewerDID)/link.latr.bookmark/saved",
            subjectUri: "at://did:plc:author/site.standard.document/story"))
    }

    private func waitForRequests(_ count: Int, in transport: SavedLinkPreviewTransport) async -> Bool {
        let deadline = ContinuousClock.now.advanced(by: .seconds(2))
        while ContinuousClock.now < deadline {
            if await transport.requestCount >= count { return true }
            try? await Task.sleep(for: .milliseconds(1))
        }
        return false
    }

    @Test("A previous login cannot paint or cache a held preview after same-DID sign-in")
    func sameViewerRelogin() async throws {
        let (model, auth, store, session, transport) = try await fixture()
        defer { model.signOut() }
        let row = save(viewerDID: session.did)
        model.savedLinks = [row]
        let previous = Task { await model.enrichSavedLinkPreview(row) }
        try #require(await waitForRequests(1, in: transport))
        model.signOut()
        try await restore(session, auth: auth, store: store)
        model.savedLinks = [row]
        let current = Task { await model.enrichSavedLinkPreview(row) }
        try #require(await waitForRequests(2, in: transport))
        try await transport.complete(0, title: "Previous Login Preview")
        await previous.value
        #expect(model.savedLinks.first?.title == row.title)
        try await transport.complete(1, title: "Current Login Preview")
        await current.value
        #expect(model.savedLinks.first?.title == "Current Login Preview")
        await model.enrichSavedLinkPreview(row)
        #expect(await transport.requestCount == 2)
    }

    @Test("A cancelled preview cannot paint a late response")
    func cancelledPreview() async throws {
        let (model, _, _, session, transport) = try await fixture()
        defer { model.signOut() }
        let row = save(viewerDID: session.did)
        model.savedLinks = [row]
        let pending = Task { await model.enrichSavedLinkPreview(row) }
        try #require(await waitForRequests(1, in: transport))
        pending.cancel()
        try await transport.complete(0, title: "Cancelled Preview")
        await pending.value
        #expect(model.savedLinks.first?.title == row.title)
    }
}
