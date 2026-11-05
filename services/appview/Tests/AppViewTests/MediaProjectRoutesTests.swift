import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import Testing

@testable import AppView

struct MediaProjectRoutesTests {
  @Test func authenticatesBeforeReadingCandidatesAndDisablesCaching() async throws {
    for owner in [nil, "", GatewayInternalTrustAuthMiddleware.anonymousDiscoveryDID] {
      let router = Router(context: GatewayRequestContext.self)
      router.add(middleware: SocialDraftTestAuth(owner: owner))
      MediaProjectRoutes().register(on: router.group())
      try await Application(router: router).test(.router) { client in
        let response = try await client.execute(
          uri: "/v1/media/project", method: .post, body: .init(string: "not json"))
        #expect(response.status.code == 401)
        #expect(response.headers[.cacheControl] == "private, no-store")
        #expect(String(buffer: response.body).contains("Authentication Required"))
      }
    }
  }

  @Test func rejectsInvalidShapeUnknownFieldsTrailingJSONAndOverBudgetInputs() async throws {
    let router = Router(context: GatewayRequestContext.self)
    router.add(middleware: SocialDraftTestAuth(owner: "did:plc:viewer"))
    MediaProjectRoutes().register(on: router.group())
    let bodies = [
      "{}", #"{"mode":"all","posts":[]}"#, #"{"mode":"photos","posts":null}"#,
      #"{"mode":"photos","posts":[],"viewerDid":"did:plc:other"}"#,
      #"{"mode":"photos","posts":[]} {}"#,
      #"{"mode":"photos","posts":[{"uri":"a","embed":{},"unknown":true}]}"#,
      #"{"mode":"photos","posts":[{"uri":12,"embed":{}}]}"#,
      #"{"mode":"photos","posts":[{"uri":"a","embed":[]}]}"#,
      #"{"mode":"photos","posts":["#
        + Array(repeating: #"{"uri":"a","embed":{}}"#, count: 31).joined(separator: ",") + "]}",
      #"{"mode":"photos","posts":[{"uri":"a","embed":{"text":""#
        + String(repeating: "a", count: 512 << 10) + #""}}]}"#,
    ]
    try await Application(router: router).test(.router) { client in
      for body in bodies {
        let response = try await client.execute(
          uri: "/v1/media/project", method: .post, body: .init(string: body))
        #expect(response.status.code == 400)
        #expect(response.headers[.cacheControl] == "private, no-store")
        #expect(String(buffer: response.body).contains("Invalid Media Candidates"))
      }
      for body in [
        #"{"mode":"photos","posts":[]}"#,
        #"{"mode":"photos","posts":[null,{"uri":null,"embed":null}]}"#,
      ] {
        let response = try await client.execute(
          uri: "/v1/media/project", method: .post, body: .init(string: body))
        #expect(response.status.code == 200)
        #expect(response.headers[.cacheControl] == "private, no-store")
        let object = try #require(
          JSONSerialization.jsonObject(with: Data(buffer: response.body)) as? [String: Any])
        #expect(object["uris"] as? [String] == [])
      }
    }
  }
}
