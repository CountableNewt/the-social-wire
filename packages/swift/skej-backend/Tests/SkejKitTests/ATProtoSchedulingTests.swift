import Foundation
import SkejKit
import Testing

private let did = "did:plc:scheduler"
private let pds = "https://pds.example"
private let issuer = "https://issuer.example"
private let stableRkey = "3npgloz4nm72k"
private let timestamp = "2026-08-01T00:00:00.000Z"

@Test func backgroundAndWebProbesShareOneTokenRefresh() async throws {
    let store = try await schedulingStore()
    let transport = RefreshFixture()
    let client = ATProtoPDSClient(store: store, clientID: "https://scheduler.example/oauth/metadata", http: transport)

    async let first: Void = client.verifySchedulingConnection(did: did)
    async let second: Void = client.verifySchedulingConnection(did: did)
    _ = try await (first, second)

    #expect(await transport.refreshes == 1)
    let persisted = try #require(try await store.oauthSession(did: did))
    let token = try JSONDecoder().decode(ATProtoTokenPayload.self, from: Data(persisted.tokenJSON.utf8))
    #expect(token.refreshToken == "rotated")
}

@Test func publishingConfirmsMatchingRecordWithoutReplacingAnExistingPost() async throws {
    let store = try await schedulingStore()
    let record = SkejScheduleRecord(scheduledAt: timestamp, createdAt: timestamp, updatedAt: timestamp,
        status: .scheduled, publishRkey: stableRkey, posts: [PostPlan(text: "Scheduled", publishRkey: stableRkey)])
    let matching = PublishFixture(text: "Scheduled")
    let client = ATProtoPDSClient(store: store, http: matching)
    let published = try await client.publishThread(did: did, record: record)
    #expect(published.map(\.rkey) == [stableRkey])
    #expect(await matching.paths == ["com.atproto.repo.createRecord", "com.atproto.repo.getRecord"])

    let conflicting = PublishFixture(text: "Different existing post")
    let conflictClient = ATProtoPDSClient(store: store, http: conflicting)
    await #expect(throws: HTTPClientError.self) {
        try await conflictClient.publishThread(did: did, record: record)
    }
    #expect(await conflicting.paths == ["com.atproto.repo.createRecord", "com.atproto.repo.getRecord"])
}

private func schedulingStore() async throws -> SQLiteStore {
    let store = try SQLiteStore(path: ":memory:")
    try await store.migrate()
    let token = ATProtoTokenPayload(accessToken: "expired", refreshToken: "original", tokenType: "DPoP",
        expiresIn: 600, scope: "atproto", sub: did, pdsEndpoint: pds, tokenEndpoint: issuer + "/oauth/token")
    try await store.createOAuthSession(OAuthSessionRecord(did: did, handle: nil,
        tokenJSON: String(decoding: try JSONEncoder().encode(token), as: UTF8.self),
        dpopKeyJSON: try DPoPKey().exportJSON()), now: timestamp)
    return store
}

private actor RefreshFixture: HTTPClient {
    private(set) var refreshes = 0

    func data(_ request: HTTPRequest) async throws -> HTTPResponseData {
        if request.url == issuer + "/oauth/token" {
            refreshes += 1
            try await Task.sleep(for: .milliseconds(35))
            let json = """
            {"access_token":"renewed","refresh_token":"rotated","token_type":"DPoP","expires_in":600,"scope":"atproto","sub":"\(did)"}
            """
            return HTTPResponseData(body: Data(json.utf8), headers: [:], statusCode: 200)
        }
        guard request.headers["Authorization"] == "DPoP renewed" else {
            throw HTTPClientError.badStatus(401, "ExpiredToken", [:])
        }
        return HTTPResponseData(body: Data("{}".utf8), headers: [:], statusCode: 200)
    }
}

private actor PublishFixture: HTTPClient {
    let text: String
    private(set) var paths: [String] = []

    init(text: String) { self.text = text }

    func data(_ request: HTTPRequest) async throws -> HTTPResponseData {
        let path = URLComponents(string: request.url)!.path.split(separator: "/").last.map(String.init)!
        paths.append(path)
        if path == "com.atproto.repo.createRecord" {
            throw HTTPClientError.badStatus(400, "RecordAlreadyExists", [:])
        }
        let json = """
        {"uri":"at://\(did)/app.bsky.feed.post/\(stableRkey)","cid":"test-cid","value":{"$type":"app.bsky.feed.post","text":"\(text)","createdAt":"\(timestamp)"}}
        """
        return HTTPResponseData(body: Data(json.utf8), headers: [:], statusCode: 200)
    }
}
