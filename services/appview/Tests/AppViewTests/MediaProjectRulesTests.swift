import Foundation
import Testing

@testable import AppView

struct MediaProjectRulesTests {
  let photo =
    #"{"$type":"app.bsky.embed.images#view","images":[{"thumb":"https://cdn.bsky.app/thumb","fullsize":"https://cdn.bsky.app/full"}]}"#
  let portrait =
    #"{"$type":"app.bsky.embed.video#view","playlist":"https://video.bsky.app/play.m3u8","aspectRatio":{"width":720,"height":1280}}"#
  let gallery =
    #"{"$type":"app.bsky.embed.gallery#view","items":[{"$type":"app.bsky.embed.gallery#viewImage","thumbnail":"https://cdn.bsky.app/thumb","fullsize":"https://cdn.bsky.app/full"}]}"#

  @Test func followsTheGoMediaSelectionFixtures() throws {
    let cases: [(String, String, Bool)] = [
      (photo, "photos", true), (gallery, "photos", true), (portrait, "short-videos", true),
      (gallery.replacingOccurrences(of: "#viewImage", with: "#viewVideo"), "photos", false),
      (
        portrait.replacingOccurrences(
          of: #""width":720,"height":1280"#, with: #""width":1280,"height":720"#), "short-videos",
        false
      ),
      (
        #"{"$type":"app.bsky.embed.video#view","playlist":"https://video.bsky.app/play"}"#,
        "short-videos", false
      ),
      (
        portrait.replacingOccurrences(of: #""width":720"#, with: #""width":0.5"#), "short-videos",
        false
      ),
      (
        portrait.replacingOccurrences(of: #""width":720"#, with: #""width":true"#), "short-videos",
        false
      ),
      (
        portrait.replacingOccurrences(
          of: #""playlist""#, with: #""presentation":"gif","playlist""#), "short-videos", false
      ),
      (
        portrait.replacingOccurrences(
          of: "https://video.bsky.app/play.m3u8", with: "javascript:alert(1)"), "short-videos",
        false
      ),
      (
        photo.replacingOccurrences(
          of: "https://cdn.bsky.app/full", with: "http://cdn.bsky.app/full"), "photos", false
      ),
      (
        photo.replacingOccurrences(
          of: "https://cdn.bsky.app/full", with: "https://user:pass@cdn.bsky.app/full"), "photos",
        false
      ),
      (#"{"$type":"app.bsky.embed.external#view"}"#, "photos", false),
      (#"{"$type":"app.bsky.embed.recordWithMedia#view","media":\#(photo)}"#, "photos", true),
      (
        #"{"$type":"app.bsky.embed.recordWithMedia#view","media":\#(portrait)}"#, "short-videos",
        true
      ),
      (
        #"{"$type":"app.bsky.embed.recordWithMedia#view","media":{"$type":"app.bsky.embed.recordWithMedia#view","media":\#(photo)}}"#,
        "photos", false
      ),
    ]
    for (raw, mode, expected) in cases {
      let object = try #require(
        JSONSerialization.jsonObject(with: Data(raw.utf8)) as? [String: Any])
      #expect(MediaProjectRules.matches(object, mode: mode) == expected, "\(raw)")
    }
  }

  @Test func deduplicatesInCandidateOrderWithoutRetainingViewerState() throws {
    let body =
      #"{"mode":"photos","posts":[{"uri":"at://did:plc:a/app.bsky.feed.post/1","embed":\#(photo)},{"uri":"at://did:plc:a/app.bsky.feed.post/1","embed":\#(photo)},{"uri":"https://evil/post","embed":\#(photo)},{"uri":"at://did:plc:b/app.bsky.feed.post/2","embed":\#(photo)}]}"#
    let result = try MediaProjectRules.project(Data(body.utf8))
    #expect(
      result["uris"] as? [String] == [
        "at://did:plc:a/app.bsky.feed.post/1", "at://did:plc:b/app.bsky.feed.post/2",
      ])
    #expect(result["source"] as? String == "bluesky-following")
    #expect(result["selection"] as? String == "images")
    #expect(result["durationVerified"] as? Bool == false)
    #expect(result["unsupportedSources"] as? [String] == ["spark", "grain", "flashes-attribution"])
    #expect(
      try MediaProjectRules.project(Data(#"{"mode":"photos","posts":[]}"#.utf8))["uris"]
        as? [String] == [])
    let video = try MediaProjectRules.project(
      Data(
        #"{"mode":"short-videos","posts":[{"uri":"at://did:plc:a/app.bsky.feed.post/1","embed":\#(portrait)}]}"#
          .utf8))
    #expect(video["selection"] as? String == "portrait-video")
  }

  @Test func matchesGoStructKeyCasingWithoutChangingEmbedKeys() throws {
    let body =
      #"{"MODE":"photos","Mode":"short-videos","POſTſ":[{"URI":"at://did:plc:a/app.bsky.feed.post/1","EMBED":\#(portrait)}]}"#
    let result = try MediaProjectRules.project(Data(body.utf8))
    #expect(result["selection"] as? String == "portrait-video")
    #expect(result["uris"] as? [String] == ["at://did:plc:a/app.bsky.feed.post/1"])
    #expect(throws: (any Error).self) {
      try MediaProjectRules.project(Data(#"{"MODE":"photos","POSTS":[],"VIEWERDID":"other"}"#.utf8))
    }
    #expect(throws: (any Error).self) {
      try MediaProjectRules.project(
        Data(#"{"MODE":"photos","POSTS":[{"URI":"a","UNKNOWN":true}]}"#.utf8))
    }
  }

  @Test func retainsGoDuplicateFieldTypeNullMapAndSliceSemantics() throws {
    let uri = "at://did:plc:a/app.bsky.feed.post/1"
    let videoHead =
      #"{"$type":"app.bsky.embed.video#view","playlist":"https://video.bsky.app/play.m3u8"}"#
    let videoRatio = #"{"aspectRatio":{"width":720,"height":1280}}"#
    let valid = [
      #"{"mode":"photos","MODE":null,"posts":[{"uri":"\#(uri)","URI":null,"embed":\#(photo)}]}"#,
      #"{"mode":"photos","posts":[{"uri":"\#(uri)","embed":\#(photo)}],"POSTS":[{}]}"#,
      #"{"mode":"photos","posts":[{"uri":"\#(uri)","embed":\#(photo)},null],"POSTS":[{}],"posts":[{},{}]}"#,
      #"{"mode":"short-videos","posts":[{"uri":"\#(uri)","embed":\#(videoHead),"EMBED":\#(videoRatio)}]}"#,
      #"{"mode":"photos","posts":[{"uri":"\#(uri)","embed":{"$type":"invalid","$type":"app.bsky.embed.images#view","images":[{"thumb":"https://cdn.bsky.app/t","fullsize":"https://cdn.bsky.app/f"}]}}]}"#,
    ]
    for body in valid {
      #expect(try MediaProjectRules.project(Data(body.utf8))["uris"] as? [String] == [uri])
    }
    for body in [
      #"{"mode":123,"mode":"photos","posts":[]}"#,
      #"{"mode":"photos","posts":"bad","posts":[]}"#,
      #"{"mode":"photos","posts":[{"uri":123,"URI":"\#(uri)","embed":\#(photo)}]}"#,
      #"{"mode":"photos","posts":[{"embed":123,"EMBED":\#(photo)}]}"#,
      #"{"mode":"photos","posts":[{"unknown":true}],"posts":[]}"#,
    ] {
      #expect(throws: (any Error).self) { try MediaProjectRules.project(Data(body.utf8)) }
    }
    let cleared =
      #"{"mode":"photos","posts":[{"uri":"\#(uri)","embed":\#(photo)}],"POSTS":[],"posts":[{}]}"#
    #expect(try MediaProjectRules.project(Data(cleared.utf8))["uris"] as? [String] == [])
  }

  @Test func boundsMediaURLsAndImageCounts() throws {
    for value in [
      "http://host.test/path", "https://user:pass@host.test/path",
      "https://host.test/path#fragment",
      "https://host.test/\\path", "https://host.test/\npath", "https://host.test/%XX",
      "https://host.test/" + String(repeating: "a", count: 8192),
    ] {
      #expect(!MediaProjectRules.safeMedia(value))
    }
    for count in [0, 4, 5] {
      let item: [String: Any] = [
        "thumb": "https://cdn.bsky.app/thumb", "fullsize": "https://cdn.bsky.app/full",
      ]
      #expect(
        MediaProjectRules.matches(
          ["$type": "app.bsky.embed.images#view", "images": Array(repeating: item, count: count)],
          mode: "photos") == (count == 4))
    }
    for count in [0, 20, 21] {
      let item: [String: Any] = [
        "$type": "app.bsky.embed.gallery#viewImage", "thumbnail": "https://cdn.bsky.app/thumb",
        "fullsize": "https://cdn.bsky.app/full",
      ]
      #expect(
        MediaProjectRules.matches(
          ["$type": "app.bsky.embed.gallery#view", "items": Array(repeating: item, count: count)],
          mode: "photos") == (count == 20))
    }
  }
}
