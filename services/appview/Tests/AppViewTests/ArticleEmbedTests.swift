import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import Testing
@testable import AppView

@Suite struct ArticleEmbedTests {
  @Test func headDiscoveryExcludesScriptsCommentsBodiesAndPublicationRecords() throws {
    let article = ArticleEmbedTestReader.article
    let publication = ArticleEmbedTestReader.publication
    let accepted = [
      "<head><meta name=\"at:alternate\" content=\"\(publication)\"><meta name=\"at:canonical\" content=\"\(article)\"></head>",
      "<HEAD><LINK REL=\"alternate site.standard.document\" HREF=\"\(article)\"></HEAD>",
      "<head><meta property='AT:CANONICAL' content='at&#58;//did:plc:author/site.standard.document/post'></head>",
    ]
    for html in accepted { #expect(try ArticleEmbedResolver.headURI(html) == article) }
    let rejected = [
      "<head><meta name=\"at:canonical\" content=\"\(publication)\"></head>",
      "<head></head><body><link rel=\"site.standard.document\" href=\"\(article)\">",
      "<head><link rel=\"alternate\" href=\"\(article)\"></head>",
      "<head><script>'<meta name=\"at:canonical\" content=\"\(article)\">'</script></head>",
      "<!-- <head><meta name=\"at:canonical\" content=\"\(article)\"></head> -->",
      "<meta name=\"at:canonical\" content=\"\(article)\">",
    ]
    for html in rejected { #expect(try ArticleEmbedResolver.headURI(html) == nil) }
  }

  @Test func validatesBacklinksPathsRecordIdentityAndSuccessOnlyCache() async throws {
    let reader = ArticleEmbedTestReader(failOnce: true)
    let resolver = ArticleEmbedResolver(reader: reader)
    await #expect(throws: ArticleEmbedError.unavailable) { try await resolver.resolve("https://publisher.social/post") }
    let value = try await resolver.resolve("https://publisher.social/post#fragment")
    #expect(value.documentUri == ArticleEmbedTestReader.article && value.authorDid == "did:plc:author")
    #expect(value.description == nil && value.articleUrl == "https://publisher.social/post")
    _ = try await resolver.resolve("https://publisher.social/post")
    #expect(await reader.calls == 2)
    await #expect(throws: ArticleEmbedError.notFound) { try await resolver.resolve("https://attacker.social/post") }
    await #expect(throws: ArticleEmbedError.invalidURL) { try await resolver.resolve("http://publisher.social/post") }
    for identity in [ArticleEmbedTestReader.article, ArticleEmbedTestReader.publication] {
      let mismatched = ArticleEmbedTestReader()
      await mismatched.mismatchIdentity(identity)
      await #expect(throws: ArticleEmbedError.notFound) { try await ArticleEmbedResolver(reader: mismatched).resolve("https://publisher.social/post") }
    }
    let subpath = ArticleEmbedResolver(reader: ArticleEmbedTestReader(base: "https://publisher.social/blog"))
    #expect(try await subpath.resolve("https://publisher.social/blog/post").publicationUrl == "https://publisher.social/blog")
    let root = ArticleEmbedResolver(reader: ArticleEmbedTestReader(path: "/"))
    #expect(try await root.resolve("https://publisher.social/").articleUrl == "https://publisher.social/")
    let unsafe = ArticleEmbedResolver(reader: ArticleEmbedTestReader(path: "//attacker.social/post"))
    await #expect(throws: ArticleEmbedError.notFound) { try await unsafe.resolve("https://attacker.social/post") }
  }

  @Test func rejectsUnauthenticatedCallsAndReturnsSafeDeadlineFailure() async throws {
    let anonymous = Router(context: GatewayRequestContext.self)
    ArticleEmbedRoutes(resolver: ArticleEmbedResolver(reader: ArticleEmbedTestReader())).register(on: anonymous.group())
    try await Application(router: anonymous).test(.router) { client in
      let response = try await client.execute(uri: "/v1/publications/article-embed?url=https://publisher.social/post", method: .get)
      #expect(response.status.code == 401)
    }
    let stalled = ArticleEmbedTestReader()
    await stalled.stallPage()
    let router = Router(context: GatewayRequestContext.self)
    router.add(middleware: SocialDraftTestAuth(owner: "did:plc:viewer"))
    ArticleEmbedRoutes(resolver: ArticleEmbedResolver(reader: stalled), timeoutSeconds: 0.03).register(on: router.group())
    try await Application(router: router).test(.router) { client in
      let start = ContinuousClock.now
      let response = try await client.execute(uri: "/v1/publications/article-embed?url=https://publisher.social/post", method: .get)
      #expect(response.status.code == 502 && response.headers[.cacheControl] == "no-store")
      #expect(start.duration(to: .now) < .seconds(2))
      let json = try #require(JSONSerialization.jsonObject(with: Data(buffer: response.body)) as? [String: String])
      #expect(json == ["error": "Bad Gateway", "message": "Verified article unavailable"])
    }
  }

  @Test func servesBothAuthenticatedRoutesWithGoWireEnvelope() async throws {
    let router = Router(context: GatewayRequestContext.self)
    router.add(middleware: SocialDraftTestAuth(owner: "did:plc:viewer"))
    ArticleEmbedRoutes(resolver: ArticleEmbedResolver(reader: ArticleEmbedTestReader())).register(on: router.group())
    try await Application(router: router).test(.router) { client in
      for path in ["/v1/publications/article-embed", "/xrpc/app.thesocialwire.publication.getArticleEmbed"] {
        let response = try await client.execute(uri: path + "?url=https%3A%2F%2Fpublisher.social%2Fpost", method: .get)
        #expect(response.status.code == 200 && response.headers[.cacheControl] == "private, max-age=300")
        let json = try #require(JSONSerialization.jsonObject(with: Data(buffer: response.body)) as? [String: Any])
        #expect(json["documentUri"] as? String == ArticleEmbedTestReader.article && json["description"] == nil)
        #expect(try await client.execute(uri: path + "?url=http://publisher.social/post", method: .get).status.code == 400)
        #expect(try await client.execute(uri: path + "?url=https://foreign.social/post", method: .get).status.code == 404)
      }
    }
  }
}
