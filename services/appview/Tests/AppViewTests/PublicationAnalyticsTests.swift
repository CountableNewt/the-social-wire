import AsyncHTTPClient
import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import NIOFoundationCompat
import Testing
@testable import AppView

@Suite struct PublicationAnalyticsTests {
  private static let author = "did:plc:creator"
  private static let publication = "at://\(author)/site.standard.publication/main"
  private static let empty = "at://\(author)/site.standard.publication/empty"

  @Test func attributesDocumentsToTheirPublicationAndIncludesEmptyPublications() async throws {
    let report = try await PublicationAnalyticsProvider(reader: Self.reader(), count: { target, collection, path in
      if collection == "site.standard.graph.subscription" {
        #expect(path == ".publication")
        return target == Self.publication ? 5 : 0
      }
      #expect(collection == "site.standard.graph.recommend")
      #expect(path == ".document")
      #expect(target.hasSuffix("/one") || target.hasSuffix("/two"))
      return target.hasSuffix("/one") ? 2 : 3
    }).summary(author: Self.author, at: Date())
    #expect(report.inventoryComplete)
    let main = try #require(report.publications.first { $0.uri == Self.publication })
    #expect(main.name == "Main")
    #expect(main.url == "https://example.com/main")
    #expect(main.documents == 2 && main.subscribers == 5 && main.recommendations == 5)
    #expect(main.recommendationCoverageComplete)
    let empty = try #require(report.publications.first { $0.uri == Self.empty })
    #expect(empty.documents == 0 && empty.subscribers == 0 && empty.recommendations == 0)
    #expect(empty.recommendationCoverageComplete)
  }

  @Test func preservesPartialRecommendationCountsAndEncodesUnavailableMetricsAsNull() async throws {
    let report = try await PublicationAnalyticsProvider(reader: Self.reader(), count: { target, _, _ in
      guard target.hasSuffix("/one") else { throw AnalyticsError.providerUnavailable }
      return 2
    }).summary(author: Self.author, at: Date())
    let main = try #require(report.publications.first { $0.uri == Self.publication })
    #expect(main.recommendations == 2 && !main.recommendationCoverageComplete)
    let encoded = try #require(JSONSerialization.jsonObject(with: AnalyticsCoding.encoder().encode(main)) as? [String: Any])
    #expect(encoded["subscribers"] is NSNull)
  }

  @Test func capsRecommendationLookupsAndMarksTheRollupPartial() async throws {
    var reader = Self.reader()
    reader.collections["site.standard.document"] = (0..<41).map { index in
      Self.document("doc\(index)", site: Self.publication)
    }
    let result = try await PublicationAnalyticsProvider(reader: reader, count: { _, _, _ in 1 })
      .summary(author: Self.author, at: Date())
    let main = try #require(result.publications.first { $0.uri == Self.publication })
    #expect(result.inventoryComplete)
    #expect(main.documents == 41 && main.recommendations == 40 && !main.recommendationCoverageComplete)
  }

  @Test func usesTheDistinctBacklinkCountAndDistinguishesZeroFromFailure() async throws {
    let reader = PublicationAnalyticsCountReader(http: .shared, transport: { url in
      #expect(url.scheme == "https" && url.host == "constellation.microcosm.blue")
      #expect(url.path == "/links/count/distinct-dids")
      let query = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems
      #expect(query == [.init(name: "target", value: Self.publication),
        .init(name: "collection", value: "site.standard.graph.subscription"),
        .init(name: "path", value: ".publication")])
      return Data(#"{"total":0}"#.utf8)
    })
    #expect(try await reader.count(target: Self.publication, collection: "site.standard.graph.subscription", path: ".publication") == 0)
    let broken = PublicationAnalyticsCountReader(http: .shared, transport: { _ in Data(#"{"total":-1}"#.utf8) })
    await #expect(throws: AnalyticsError.providerUnavailable) {
      try await broken.count(target: Self.publication, collection: "site.standard.graph.subscription", path: ".publication")
    }
  }

  @Test(arguments: [false, true])
  func requiresAuthenticationAndUsesOnlyTheAuthenticatedRepository(authenticated: Bool) async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".sqlite").path
    defer { for suffix in ["", "-wal", "-shm"] { try? FileManager.default.removeItem(atPath: path + suffix) } }
    let store = try SQLiteAnalyticsStore(path: path, secret: String(repeating: "fixture-secret", count: 4))
    let router = Router(context: GatewayRequestContext.self)
    if authenticated { router.add(middleware: SocialDraftTestAuth(owner: Self.author)) }
    let publications = PublicationAnalyticsProvider(reader: Self.reader(), count: { _, _, _ in
      #expect(authenticated)
      return 0
    })
    AnalyticsRoutes(store: store, provider: AnalyticsTestContent(), publications: publications).register(on: router.group())
    try await Application(router: router).test(.router) { client in
      let response = try await client.execute(uri: "/v1/analytics/publications?author=did:plc:foreign", method: .get)
      #expect(response.headers[.cacheControl] == "no-store")
      if authenticated {
        #expect(response.status == .ok)
        let report = try AnalyticsCoding.decoder().decode(PublicationAnalyticsSummary.self, from: Data(buffer: response.body))
        #expect(report.publications.map(\.uri) == [Self.publication, Self.empty])
      } else {
        #expect(response.status == .unauthorized)
      }
    }
  }

  private static func reader() -> AnalyticsTestReader {
    .init(collections: [
      "site.standard.publication": [
        .init(uri: publication, value: .init(values: ["$type": "site.standard.publication", "name": "Main", "url": "https://example.com/main"])),
        .init(uri: empty, value: .init(values: ["$type": "site.standard.publication", "name": "Empty", "url": "https://example.com/empty"])),
      ],
      "site.standard.document": [
        document("one", site: publication),
        document("two", site: publication),
        document("outside", site: "at://did:plc:other/site.standard.publication/outside"),
      ],
    ])
  }

  private static func document(_ rkey: String, site: String) -> AnalyticsPublicRecord {
    .init(uri: "at://\(author)/site.standard.document/\(rkey)",
      value: .init(values: ["$type": "site.standard.document", "site": site]))
  }
}
