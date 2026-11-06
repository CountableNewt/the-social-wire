import Foundation

struct SocialDraftConfiguration: Sendable {
  let endpoint: String
  let bucket: String
  let accessKeyID: String
  let secretAccessKey: String
  let region: String
  let storageKey: Data

  static func load(_ env: [String: String]) throws -> Self? {
    guard env["SOCIAL_DRAFTS_ENABLED"]?.lowercased() == "true" else { return nil }
    guard let endpoint = env["SOCIAL_DRAFT_S3_ENDPOINT"], let url = URLComponents(string: endpoint),
      url.scheme == "https", let host = url.host, !host.isEmpty, url.user == nil, url.password == nil,
      url.query == nil, url.fragment == nil,
      let bucket = env["SOCIAL_DRAFT_S3_BUCKET"], !bucket.isEmpty,
      let access = env["SOCIAL_DRAFT_S3_ACCESS_KEY_ID"], !access.isEmpty,
      let secret = env["SOCIAL_DRAFT_S3_SECRET_ACCESS_KEY"], !secret.isEmpty,
      let region = env["SOCIAL_DRAFT_S3_REGION"], !region.isEmpty,
      let encodedKey = env["SOCIAL_DRAFT_STORAGE_KEY"], let key = Data(base64Encoded: encodedKey),
      key.count == 32, key.base64EncodedString() == encodedKey else {
      throw SocialDraftStorageError.unavailable
    }
    return .init(endpoint: endpoint, bucket: bucket, accessKeyID: access,
      secretAccessKey: secret, region: region, storageKey: key)
  }
}
