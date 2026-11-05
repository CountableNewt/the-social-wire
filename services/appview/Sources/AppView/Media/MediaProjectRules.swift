import CoreFoundation
import Foundation

enum MediaProjectRules {
  static let maximumBytes = 512 << 10

  static func project(_ data: Data) throws -> [String: Any] {
    guard data.count <= maximumBytes else { throw MediaProjectError.invalidCandidates }
    let input = try MediaProjectJSON.request(data)
    let mode = input.mode
    guard ["photos", "short-videos"].contains(mode) else {
      throw MediaProjectError.invalidCandidates
    }
    var uris: [String] = []
    var seen: Set<String> = []
    for post in input.posts {
      if validPostURI(post.uri), !seen.contains(post.uri), matches(post.embed, mode: mode) {
        seen.insert(post.uri)
        uris.append(post.uri)
      }
    }
    return [
      "uris": uris, "source": "bluesky-following",
      "selection": mode == "short-videos" ? "portrait-video" : "images",
      "durationVerified": false, "unsupportedSources": ["spark", "grain", "flashes-attribution"],
    ]
  }

  static func validPostURI(_ raw: String) -> Bool {
    guard raw.utf8.count <= 2048, raw.hasPrefix("at://did:"),
      !raw.contains(where: { "?# \t\r\n".contains($0) })
    else { return false }
    let parts = raw.dropFirst(5).split(separator: "/", omittingEmptySubsequences: false)
    return parts.count == 3 && parts[0] != "did:" && parts[1] == "app.bsky.feed.post"
      && !parts[2].isEmpty
  }

  static func safeMedia(_ raw: Any?) -> Bool {
    guard let value = raw as? String, value.utf8.count <= 8192,
      !value.contains("\\"), !value.utf8.contains(where: { $0 < 32 || $0 == 127 }),
      let url = URLComponents(string: value), url.scheme?.lowercased() == "https",
      url.host?.isEmpty == false, url.user == nil, url.password == nil,
      url.fragment == nil || url.fragment == ""
    else { return false }
    // Foundation repairs invalid percent escapes. net/url rejects them in the
    // authority and path (the query is deliberately opaque in both contracts).
    let path = value.split(separator: "?", maxSplits: 1, omittingEmptySubsequences: false)[0]
    let bytes = Array(path.utf8)
    for index in bytes.indices where bytes[index] == 37 {
      guard index + 2 < bytes.count,
        bytes[(index + 1)...(index + 2)].allSatisfy({
          (48...57).contains($0) || (65...70).contains($0) || (97...102).contains($0)
        })
      else { return false }
    }
    return true
  }

  static func matches(_ embed: [String: Any], mode: String, depth: Int = 0) -> Bool {
    guard depth <= 1 else { return false }
    switch embed["$type"] as? String {
    case "app.bsky.embed.recordWithMedia#view":
      return matches(embed["media"] as? [String: Any] ?? [:], mode: mode, depth: depth + 1)
    case "app.bsky.embed.images#view":
      return mode == "photos" && validImages(embed["images"] as? [Any] ?? [], gallery: false)
    case "app.bsky.embed.gallery#view":
      return mode == "photos" && validImages(embed["items"] as? [Any] ?? [], gallery: true)
    case "app.bsky.embed.video#view":
      let ratio = embed["aspectRatio"] as? [String: Any] ?? [:]
      guard mode == "short-videos", let width = integer(ratio["width"]),
        let height = integer(ratio["height"]), width > 0, height > width,
        safeMedia(embed["playlist"]), embed["presentation"] as? String != "gif"
      else { return false }
      return embed["thumbnail"] == nil || embed["thumbnail"] is NSNull
        || safeMedia(embed["thumbnail"])
    default: return false
    }
  }

  private static func integer(_ raw: Any?) -> Double? {
    guard let value = raw as? NSNumber, CFGetTypeID(value) != CFBooleanGetTypeID() else {
      return nil
    }
    let number = value.doubleValue
    return number.isFinite && number.rounded(.towardZero) == number ? number : nil
  }

  private static func validImages(_ items: [Any], gallery: Bool) -> Bool {
    guard !items.isEmpty, items.count <= (gallery ? 20 : 4) else { return false }
    return items.allSatisfy { raw in
      guard let item = raw as? [String: Any],
        !gallery || item["$type"] as? String == "app.bsky.embed.gallery#viewImage"
      else { return false }
      return safeMedia(item[gallery ? "thumbnail" : "thumb"]) && safeMedia(item["fullsize"])
    }
  }
}
