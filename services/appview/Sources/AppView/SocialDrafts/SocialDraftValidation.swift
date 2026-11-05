import Foundation

enum SocialDraftValidation {
  static let maximumFileBytes: Int64 = 100 << 20
  static let maximumAccountBytes: Int64 = 1 << 30

  static func owner(_ value: String) -> Bool {
    value.hasPrefix("did:") && value.utf8.count <= 2048 && !value.contains(where: { "\0\r\n".contains($0) })
  }

  static func assetID(_ value: String) -> Bool {
    value.range(of: "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", options: .regularExpression) != nil
  }

  static func draftID(_ value: String) -> Bool {
    value.range(of: "^[234567abcdefghij][234567abcdefghijklmnopqrstuvwxyz]{12}$", options: .regularExpression) != nil
  }

  static func reserve(_ input: SocialDraftReserveInput) -> Bool {
    assetID(input.assetId) && !input.name.isEmpty && input.name.utf8.count <= 1024
      && !input.name.contains("\0") && input.byteLength > 0 && input.byteLength <= maximumFileBytes
      && input.sha256.range(of: "^[0-9a-f]{64}$", options: .regularExpression) != nil
      && ["photo", "poster", "gif", "video", "caption"].contains(input.kind)
      && ["image/jpeg", "image/png", "image/webp", "image/gif", "video/mp4", "video/webm", "text/vtt"].contains(input.contentType)
  }

  static func references(_ refs: [SocialDraftManifestReference]) -> Bool {
    refs.count <= 800 && Set(refs).count == refs.count && refs.allSatisfy {
      assetID($0.assetId) && $0.ref.utf8.count <= 512 && !$0.ref.contains("\0")
        && ["original", "prepared", "video", "poster", "caption", "gif"].contains($0.role)
    }
  }
}
