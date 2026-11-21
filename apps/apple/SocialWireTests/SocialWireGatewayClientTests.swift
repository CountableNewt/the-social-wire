import Foundation
import Testing
@testable import SocialWire

@Suite("SocialWireGatewayClient")
@MainActor
struct SocialWireGatewayClientTests {
    private func fixture(_ replies: [OAuthTestTransport.Reply], delay: Duration = .zero) async throws -> (ATProtoOAuthService, SocialWireGatewayClient, OAuthTestTransport) {
        let store = OAuthTestSessionStore()
        let auth = ATProtoOAuthService(keychain: store)
        let session = AuthSession(did: "did:plc:test", pdsURL: URL(string: "https://pds.example.test")!,
            tokenEndpoint: URL(string: "https://issuer.example/token")!, accessToken: "access",
            refreshToken: "refresh", tokenType: "DPoP", scope: ATProtoOAuthService.scopes, expiresAt: .distantFuture)
        let record: [String: Any] = [
            "session": try JSONSerialization.jsonObject(with: JSONEncoder().encode(session)),
            "dpopPrivateKey": Data(repeating: 1, count: 32).base64EncodedString(),
            "clientID": ATProtoOAuthConfig.clientID,
        ]
        store.set(try JSONSerialization.data(withJSONObject: record).base64EncodedString(), for: "oauth.session.v2")
        await auth.restoreSession()
        let transport = OAuthTestTransport(replies, delay: delay)
        let gateway = SocialWireGatewayClient(auth: auth, baseURL: URL(string: "https://gateway.example.test")!, transport: { try await transport.send($0) })
        return (auth, gateway, transport)
    }

    @Test("Media projection sends only candidate embeds and retains explicit source limitations")
    func mediaProjectionContract() async throws {
        let body = #"{"uris":["at://did:plc:test/app.bsky.feed.post/one"],"source":"bluesky-following","selection":"images","durationVerified":false,"unsupportedSources":["spark","grain","flashes-attribution"]}"#
        let (auth, gateway, transport) = try await fixture([.init(status: 200, body: body)])
        defer { auth.signOut() }
        let candidate = MediaProjectionCandidate(uri: "at://did:plc:test/app.bsky.feed.post/one",
            embed: .object(["$type": .string("app.bsky.embed.images#view"), "images": .array([])]))
        let result = try await gateway.projectMedia(.init(mode: .photos, posts: [candidate]), expectedViewer: "did:plc:test")
        #expect(result.uris == [candidate.uri])
        #expect(!result.durationVerified)
        #expect(result.unsupportedSources.contains("grain"))
        let requests = await transport.recordedRequests()
        let sent = try #require(requests.first)
        #expect(sent.url?.path == "/v1/media/project")
        #expect(sent.httpMethod == "POST")
        #expect(sent.value(forHTTPHeaderField: "DPoP") != nil)
        let json = try JSONDecoder().decode(JSONValue.self, from: #require(sent.httpBody))
        #expect(Set(try #require(json.object).keys) == ["mode", "posts"])
        let post = try #require(json.object?["posts"]?.array?.first?.object)
        #expect(Set(post.keys) == ["uri", "embed"])
    }

    @Test("Media projection fails closed for changed accounts, oversized batches, and invalid source responses")
    func mediaProjectionBoundaries() async throws {
        let badBody = #"{"uris":["at://did:plc:other/app.bsky.feed.post/unrequested"],"source":"bluesky-following","selection":"images","durationVerified":false,"unsupportedSources":[]}"#
        let (auth, gateway, transport) = try await fixture([.init(status: 200, body: badBody)])
        defer { auth.signOut() }
        let candidate = MediaProjectionCandidate(uri: "at://did:plc:test/app.bsky.feed.post/one", embed: .null)
        await #expect(throws: (any Error).self) {
            _ = try await gateway.projectMedia(.init(mode: .photos, posts: [candidate]), expectedViewer: "did:plc:other")
        }
        #expect(await transport.recordedRequests().isEmpty)
        await #expect(throws: (any Error).self) {
            _ = try await gateway.projectMedia(.init(mode: .photos, posts: Array(repeating: candidate, count: 31)), expectedViewer: "did:plc:test")
        }
        #expect(await transport.recordedRequests().isEmpty)
        let oversized = MediaProjectionCandidate(uri: candidate.uri, embed: .string(String(repeating: "x", count: 512 * 1024)))
        await #expect(throws: (any Error).self) {
            _ = try await gateway.projectMedia(.init(mode: .photos, posts: [oversized]), expectedViewer: "did:plc:test")
        }
        #expect(await transport.recordedRequests().isEmpty)
        await #expect(throws: (any Error).self) {
            _ = try await gateway.projectMedia(.init(mode: .photos, posts: [candidate]), expectedViewer: "did:plc:test")
        }
        #expect(await transport.recordedRequests().count == 1)
    }

    @Test("Post-write preference reads bypass both gateway and conditional caches")
    func postWritePreferencesRequireFreshSnapshot() {
        let request = SocialWireGatewayClient.preferencesSyncRequest(
            ifNoneMatch: "\"previous-revision\"",
            forceRefresh: true
        )
        #expect(request.query["fresh"] == "true")
        #expect(request.ifNoneMatch == nil)
    }

    @Test("Ordinary preference reads retain conditional cache validation")
    func ordinaryPreferencesKeepConditionalCache() {
        let request = SocialWireGatewayClient.preferencesSyncRequest(
            ifNoneMatch: "\"current-revision\"",
            forceRefresh: false
        )
        #expect(request.query.isEmpty)
        #expect(request.ifNoneMatch == "\"current-revision\"")
    }

    @Test("Read-age snapshots stream before completion and replace prior counts")
    func readAgeSnapshotsStreamProgressively() async throws {
        let (stream, continuation) = AsyncThrowingStream<String, Error>.makeStream()
        continuation.yield(#"{"type":"options","referenceDay":"2026-09-07","options":[{"days":7,"before":"2026-08-31T05:00:00Z","count":2}]}"#)
        var snapshots: [[FeedReadAgeOption]] = []
        let response = try await FeedReadAgeStreamEvent.consume(stream) { options in
            snapshots.append(options)
            if snapshots.count == 1 {
                // No next event exists until the first snapshot is delivered to the menu.
                continuation.yield(#"{"type":"options","referenceDay":"2026-09-07","options":[{"days":7,"before":"2026-08-31T05:00:00Z","count":5}]}"#)
            } else {
                continuation.yield(#"{"type":"done"}"#)
                continuation.finish()
            }
        }
        #expect(snapshots.map { $0[0].count } == [2, 5])
        #expect(response.options[0].count == 5)
        #expect(response.options[0].title == "1 Week")
    }

    @Test("Read-age partial responses require a terminal done event")
    func readAgeTruncationFails() async {
        let stream = AsyncThrowingStream<String, Error> { continuation in
            continuation.yield(#"{"type":"options","referenceDay":"2026-09-07","options":[]}"#)
            continuation.finish()
        }
        await #expect(throws: (any Error).self) {
            try await FeedReadAgeStreamEvent.consume(stream) { _ in }
        }
    }

    @Test("Read-age server errors fail even after options arrive")
    func readAgeTerminalErrorFails() async {
        let stream = AsyncThrowingStream<String, Error> { continuation in
            continuation.yield(#"{"type":"options","referenceDay":"2026-09-07","options":[]}"#)
            continuation.yield(#"{"type":"error","message":"Read query failed"}"#)
            continuation.finish()
        }
        await #expect(throws: (any Error).self) {
            try await FeedReadAgeStreamEvent.consume(stream) { _ in }
        }
    }

    @Test("Read-age done without options fails")
    func readAgeMissingSnapshotFails() async {
        let stream = AsyncThrowingStream<String, Error> { continuation in
            continuation.yield(#"{"type":"done"}"#)
            continuation.finish()
        }
        await #expect(throws: (any Error).self) {
            try await FeedReadAgeStreamEvent.consume(stream) { _ in }
        }
    }

    @Test("AppViewEnrollResponse decodes indexed count")
    func appViewEnrollResponseDecodesIndexedCount() throws {
        let data = Data("""
        {"indexed": 42}
        """.utf8)
        let decoded = try JSONDecoder().decode(AppViewEnrollResponse.self, from: data)
        #expect(decoded.indexed == 42)
    }

    @Test("PublicationSidebarResponseDTO decodes appViewScope and unread counts")
    func publicationSidebarDecodesScopeAndUnread() throws {
        let data = Data("""
        {
          "viewerDid": "did:plc:viewer",
          "allPublicationRows": [{
            "publicationId": "at://did:plc:author/site.standard.publication/p1",
            "authorDid": "did:plc:author",
            "title": "Pub",
            "discoveredAt": "2026-01-01T00:00:00.000Z",
            "unreadCount": 3,
            "appViewScope": {
              "authorDid": "did:plc:author",
              "publicationAtUri": "at://did:plc:author/site.standard.publication/p1",
              "publicationScopeAtUris": ["at://did:plc:author/com.standard.publication/p1"],
              "publicationSiteUrls": ["https://example.com"]
            }
          }],
          "folderSections": [{
            "folderRkey": "abc",
            "folderUri": "at://did:plc:viewer/app.thesocialwire.folder/abc",
            "publications": []
          }],
          "myPublications": [],
          "subscribedUnfoldered": [],
          "followingTabPublications": [],
          "enrollAuthorDids": ["did:plc:author"],
          "refreshedAt": "2026-01-01T00:00:00.000Z",
          "unreadCountsByPublicationId": {
            "at://did:plc:author/site.standard.publication/p1": 3
          }
        }
        """.utf8)
        let decoded = try JSONDecoder().decode(PublicationSidebarResponseDTO.self, from: data)
        #expect(decoded.allPublicationRows.count == 1)
        #expect(decoded.allPublicationRows[0].unreadCount == 3)
        #expect(decoded.folderSections?.count == 1)
        let counts = PublicationProjectionMapping.unreadCountsMap(from: decoded)
        #expect(counts["at://did:plc:author/site.standard.publication/p1"] == 3)
    }

    @Test("unreadCountsMap prefers unreadCountsByPublicationId over embedded row count")
    func unreadCountsMapPrefersRecordMap() throws {
        let data = Data("""
        {
          "viewerDid": "did:plc:viewer",
          "allPublicationRows": [{
            "publicationId": "did:plc:alice",
            "authorDid": "did:plc:alice",
            "title": "Alice",
            "discoveredAt": "2026-01-01T00:00:00.000Z",
            "unreadCount": 4,
            "appViewScope": {
              "authorDid": "did:plc:alice",
              "publicationAtUri": null,
              "publicationScopeAtUris": [],
              "publicationSiteUrls": []
            }
          }],
          "myPublications": [],
          "subscribedUnfoldered": [],
          "followingTabPublications": [],
          "enrollAuthorDids": [],
          "refreshedAt": "2026-01-01T00:00:00.000Z",
          "unreadCountsByPublicationId": { "did:plc:alice": 1 }
        }
        """.utf8)
        let decoded = try JSONDecoder().decode(PublicationSidebarResponseDTO.self, from: data)
        let counts = PublicationProjectionMapping.unreadCountsMap(from: decoded)
        #expect(counts["did:plc:alice"] == 1)
    }

    @Test("unreadCountsMap does not resurrect stale embedded counts when record map is empty")
    func unreadCountsMapIgnoresStaleEmbeddedWhenRecordEmpty() throws {
        let data = Data("""
        {
          "viewerDid": "did:plc:viewer",
          "allPublicationRows": [{
            "publicationId": "did:plc:alice",
            "authorDid": "did:plc:alice",
            "title": "Alice",
            "discoveredAt": "2026-01-01T00:00:00.000Z",
            "unreadCount": 2,
            "appViewScope": {
              "authorDid": "did:plc:alice",
              "publicationAtUri": null,
              "publicationScopeAtUris": [],
              "publicationSiteUrls": []
            }
          }],
          "myPublications": [],
          "subscribedUnfoldered": [],
          "followingTabPublications": [],
          "enrollAuthorDids": [],
          "refreshedAt": "2026-01-01T00:00:00.000Z",
          "unreadCountsByPublicationId": {}
        }
        """.utf8)
        let decoded = try JSONDecoder().decode(PublicationSidebarResponseDTO.self, from: data)
        let counts = PublicationProjectionMapping.unreadCountsMap(from: decoded)
        #expect(counts["did:plc:alice"] == nil)
    }

    @Test("AppViewEntryListResponse decodes entries")
    func appViewEntryListResponseDecodesEntries() throws {
        let data = Data("""
        {"entries":[{"entryId":"at://did/site.standard.document/a","title":"A","publishedAt":"2026-01-01T00:00:00.000Z","originalUrl":"https://example.com/a","isRead":true}],"cursor":null}
        """.utf8)
        let decoded = try JSONDecoder().decode(AppViewEntryListResponse.self, from: data)
        #expect(decoded.entries.count == 1)
        #expect(decoded.entries[0].title == "A")
        #expect(decoded.entries[0].originalUrl == "https://example.com/a")
        #expect(decoded.entries[0].isRead)
    }

    @Test("AppViewEntryDetailDTO decodes flat AppView entry payload")
    func appViewEntryDetailDTODecodesFlatPayload() throws {
        let data = Data("""
        {"entryId":"at://did/site.standard.document/a","title":"A","publishedAt":"2026-01-01T00:00:00.000Z","contentHtml":"<p>Hi</p>","originalUrl":"https://example.com/a","isRead":false}
        """.utf8)
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let dto = try decoder.decode(AppViewEntryDetailDTO.self, from: data)
        let entry = dto.toEntryDetail()
        #expect(entry.title == "A")
        #expect(entry.contentHtml == "<p>Hi</p>")
        #expect(entry.embedUrl == "https://example.com/a")
    }

    @Test("AppViewUnreadCountsResponse decodes count metadata")
    func appViewUnreadCountsResponseDecodesCounts() throws {
        let data = Data("""
        {"counts":{"at://did/site.standard.publication/p1":2},"generation":42,"accuracy":"exact","countedAt":"2026-01-01T00:00:00.000Z"}
        """.utf8)
        let decoded = try JSONDecoder().decode(AppViewUnreadCountsResponse.self, from: data)
        #expect(decoded.counts?["at://did/site.standard.publication/p1"] == 2)
        #expect(decoded.generation == 42)
        #expect(decoded.accuracy == "exact")
        #expect(decoded.countedAt == "2026-01-01T00:00:00.000Z")
    }

    @Test("GatewayMarkAllReadResponseDTO decodes confirmed boundaries")
    func gatewayMarkAllReadResponseDecodesMarked() throws {
        let data = Data("""
        {
          "marked": 7,
          "confirmedAt": "2026-07-28T20:00:00.000Z",
          "boundaries": [{
            "publicationId": "at://did/site.standard.publication/main",
            "createdAt": "2026-07-28T19:59:00.000Z",
            "entryId": "at://did/site.standard.document/latest"
          }],
          "unreadCounts": {"at://did/site.standard.publication/main": 0}
        }
        """.utf8)
        let decoded = try JSONDecoder().decode(GatewayMarkAllReadResponseDTO.self, from: data)
        #expect(decoded.marked == 7)
        #expect(decoded.boundaries.first?.entryId == "at://did/site.standard.document/latest")
        #expect(decoded.unreadCounts["at://did/site.standard.publication/main"] == 0)
    }
}
