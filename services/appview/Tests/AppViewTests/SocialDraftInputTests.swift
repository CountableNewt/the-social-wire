import Foundation
import Hummingbird
import HummingbirdTesting
import GatewayCore
import Testing
@testable import AppView

@Suite struct SocialDraftInputTests {
  @Test func enabledStorageFailsClosedForIncompleteOrUnsafeConfiguration() throws {
    #expect(try SocialDraftConfiguration.load([:]) == nil)
    let complete = ["SOCIAL_DRAFTS_ENABLED": "true", "SOCIAL_DRAFT_S3_ENDPOINT": "https://bucket.example",
      "SOCIAL_DRAFT_S3_BUCKET": "private", "SOCIAL_DRAFT_S3_ACCESS_KEY_ID": "fixture",
      "SOCIAL_DRAFT_S3_SECRET_ACCESS_KEY": "fixture", "SOCIAL_DRAFT_S3_REGION": "us-east-1",
      "SOCIAL_DRAFT_STORAGE_KEY": Data(repeating: 1, count: 32).base64EncodedString()]
    #expect(try SocialDraftConfiguration.load(complete) != nil)
    for key in complete.keys where key != "SOCIAL_DRAFTS_ENABLED" {
      var invalid = complete
      invalid.removeValue(forKey: key)
      #expect(throws: SocialDraftStorageError.unavailable) { try SocialDraftConfiguration.load(invalid) }
    }
    for endpoint in ["http://bucket.example", "https://user:pass@bucket.example", "https://bucket.example?token=secret", "https://bucket.example/#fragment"] {
      var invalid = complete
      invalid["SOCIAL_DRAFT_S3_ENDPOINT"] = endpoint
      #expect(throws: SocialDraftStorageError.unavailable) { try SocialDraftConfiguration.load(invalid) }
    }
  }

  @Test func validatesCaptionsWithoutBinaryControlsOrUnboundedLines() throws {
    for valid in ["WEBVTT", "\u{feff}WEBVTT\r\n\r\n00:00.000 --> 00:01.000\r\nHello\r\n", "WEBVTT Header\n"] {
      try validateCaption(Data(valid.utf8))
    }
    for invalid in [Data(), Data("not vtt\n".utf8), Data("WEBVTT\n\0".utf8), Data("WEBVTT\n".utf8) + Data([0xff]),
      Data("WEBVTT\n".utf8) + Data(repeating: 65, count: 1 << 20)] {
      #expect(throws: SocialDraftStorageError.invalid) { try validateCaption(invalid) }
    }
  }

  @Test func unknownAndUnauthenticatedCallersCannotAccessPrivateStorage() async throws {
    for owner in [nil, GatewayInternalTrustAuthMiddleware.anonymousDiscoveryDID, "did:plc:viewer"] {
      let router = Router(context: GatewayRequestContext.self)
      router.add(middleware: SocialDraftTestAuth(owner: owner))
      SocialDraftRoutes(service: nil).register(on: router.group())
      let app = Application(router: router, configuration: .init(address: .hostname("127.0.0.1", port: 0)))
      try await app.test(.live) { client in
        let response = try await client.execute(uri: "/xrpc/app.thesocialwire.socialDraft.getManifest?draftId=3abcdefghijkl", method: .get)
        #expect(response.status.code == (owner == "did:plc:viewer" ? 503 : 401))
        #expect(response.headers[.cacheControl] == "private, no-store")
      }
    }
  }

  private func validateCaption(_ data: Data) throws {
    let file = try SocialDraftTemporaryFile()
    try data.write(to: file.url)
    try SocialDraftContentValidation.validate(file: file, asset: .init(assetId: UUID().uuidString.lowercased(),
      name: "caption.vtt", contentType: "text/vtt", sha256: "", kind: "caption", state: "reserved", byteLength: Int64(data.count)))
  }
}
