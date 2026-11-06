import Foundation

enum GIFMediaPolicy {
  static func validID(_ value: String) -> Bool {
    !value.isEmpty && value.utf8.count <= 128 && value.utf8.allSatisfy {
      (65...90).contains($0) || (97...122).contains($0) || (48...57).contains($0) || $0 == 45 || $0 == 95
    }
  }

  static func mediaURL(_ raw: String) -> URL? {
    guard let url = URLComponents(string: raw), url.scheme == "https", url.host == "static.klipy.com",
      url.port == nil, url.user == nil, url.password == nil, url.fragment == nil,
      url.path.hasPrefix("/ii/") else { return nil }
    return url.url
  }

  static func fileURL(_ raw: String) -> URL? {
    guard let url = mediaURL(raw), let components = URLComponents(url: url, resolvingAgainstBaseURL: false),
      components.query == nil, components.percentEncodedPath == components.path,
      components.path != "/ii/", URL(fileURLWithPath: components.path).standardized.path == components.path
    else { return nil }
    return url
  }

  static func normalize(_ record: GIFProviderRecord) -> GIFAsset? {
    guard let id = record.id, validID(id), let formats = record.media_formats,
      let gif = formats["gif"], let gifURL = gif.url, let dims = gif.dims,
      dims.count == 2, dims.allSatisfy({ $0 > 0 && $0 <= 20_000 }),
      let full = mediaURL(gifURL), let posterRaw = formats["preview"]?.url,
      let poster = mediaURL(posterRaw)
    else { return nil }
    let mp4 = formats["mp4"]?.url.flatMap(mediaURL)
    let webm = formats["webm"]?.url.flatMap(mediaURL)
    guard mp4 != nil || webm != nil else { return nil }
    for url in [mp4, webm].compactMap({ $0 }) {
      guard url.deletingLastPathComponent().path == full.deletingLastPathComponent().path else { return nil }
    }
    return GIFAsset(id: id, title: record.title ?? "", description: record.content_description ?? "", url: full.absoluteString, poster: poster.absoluteString, mp4: mp4?.absoluteString, webm: webm?.absoluteString, width: dims[0], height: dims[1], attribution: "Powered by KLIPY")
  }

  static func allowedMIME(_ mime: String, rendition: String) -> Bool {
    switch rendition {
    case "gif": return mime == "image/gif"
    case "mp4": return mime == "video/mp4"
    case "webm": return mime == "video/webm"
    case "poster": return ["image/jpeg", "image/png", "image/webp"].contains(mime)
    default: return false
    }
  }

  static func detectedMIME(_ head: Data) -> String? {
    let bytes = [UInt8](head)
    if bytes.starts(with: Array("GIF87a".utf8)) || bytes.starts(with: Array("GIF89a".utf8)) { return "image/gif" }
    if bytes.starts(with: [0xff, 0xd8, 0xff]) { return "image/jpeg" }
    if bytes.starts(with: [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]) { return "image/png" }
    if bytes.count >= 14 && bytes[0..<4].elementsEqual(Array("RIFF".utf8)) && bytes[8..<14].elementsEqual(Array("WEBPVP".utf8)) { return "image/webp" }
    if bytes.count >= 12, bytes[4..<8].elementsEqual(Array("ftyp".utf8)) {
      let boxSize = bytes[0..<4].reduce(0) { ($0 << 8) | Int($1) }
      if boxSize >= 12, boxSize <= bytes.count, boxSize % 4 == 0 {
        for offset in stride(from: 8, to: boxSize, by: 4) where offset != 12 {
          if bytes[offset..<offset + 3].elementsEqual(Array("mp4".utf8)) { return "video/mp4" }
        }
      }
    }
    if bytes.starts(with: [0x1a, 0x45, 0xdf, 0xa3]) { return "video/webm" }
    return nil
  }
}
