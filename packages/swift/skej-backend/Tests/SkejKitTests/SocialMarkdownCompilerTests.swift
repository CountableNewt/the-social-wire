import Foundation
@testable import SkejKit
import Testing

private struct SocialMarkdownFixture: Decodable {
    let profile: String
    let cases: [Case]

    struct Case: Decodable {
        let name: String
        let source: String
        let text: String
        let graphemeCount: Int?
        let valid: Bool?
        let mentions: [ResolvedMention]?
        let facets: [Facet]
    }

    struct Facet: Codable, Equatable {
        let byteStart: Int
        let byteEnd: Int
        let type: String
        let value: String
    }
}

@Test func socialMarkdownMatchesSharedGoldenCorpus() throws {
    let fixture = try loadSocialMarkdownFixture()
    #expect(fixture.profile == "skej.social-markdown.v1")

    for testCase in fixture.cases {
        let compilation = SocialMarkdownCompiler.compile(
            testCase.source,
            resolvedMentions: testCase.mentions ?? []
        )
        #expect(compilation.text == testCase.text, Comment(rawValue: testCase.name))
        #expect(
            compilation.facets.compactMap(goldenFacet) == testCase.facets,
            Comment(rawValue: testCase.name)
        )
        if let graphemeCount = testCase.graphemeCount {
            #expect(compilation.text.count == graphemeCount, Comment(rawValue: testCase.name))
            #expect(testCase.valid == (compilation.text.count <= 300), Comment(rawValue: testCase.name))
        }
    }
}

@Test func postPlanPreservesMarkdownSourceStableRkeyAndUnknownFields() throws {
    let data = Data(#"{"text":"Projected copy","source":{"format":"markdown","text":"**Projected** copy"},"publishRkey":"3m123456789ab","future":{"enabled":true}}"#.utf8)
    let plan = try JSONDecoder().decode(PostPlan.self, from: data)

    #expect(plan.text == "Projected copy")
    #expect(plan.source == PostSource(format: .markdown, text: "**Projected** copy"))
    #expect(plan.publishRkey == "3m123456789ab")
    #expect(plan.unknownFields["future"] == .object(["enabled": .bool(true)]))

    let encoded = try JSONEncoder().encode(plan)
    let object = try #require(JSONSerialization.jsonObject(with: encoded) as? [String: Any])
    #expect((object["future"] as? [String: Bool])?["enabled"] == true)
    #expect((object["source"] as? [String: String])?["format"] == "markdown")
    #expect(object["publishRkey"] as? String == "3m123456789ab")
}

@Test func legacyScheduleAliasesDecodeAlongsideOrderedPublishedPosts() throws {
    let legacyData = Data(#"{"$type":"at.skej.schedule","scheduledAt":"2026-08-15T12:00:00Z","timezonePolicy":"absolute_utc","createdAt":"2026-08-15T11:00:00Z","updatedAt":"2026-08-15T11:00:00Z","status":"published","recordType":"app.bsky.feed.post","publishRkey":"3m123456789ab","publishedUri":"at://did:plc:test/app.bsky.feed.post/3m123456789ab","publishedCid":"bafylegacy","retry":{"attemptCount":0},"dependency":{"dependsOnScheduleUri":"at://did:plc:test/at.skej.schedule/parent"},"posts":[{"text":"Legacy"}]}"#.utf8)
    let legacy = try JSONDecoder().decode(SkejScheduleRecord.self, from: legacyData)

    #expect(legacy.publishedPosts.isEmpty)
    #expect(legacy.publishedUri == "at://did:plc:test/app.bsky.feed.post/3m123456789ab")
    #expect(legacy.publishedCid == "bafylegacy")
    #expect(legacy.dependency?.relationship == .after)
    #expect(legacy.dependency?.parentPublishedCid == nil)

    var modern = legacy
    modern.publishedPosts = [
        PublishedPostReference(
            rkey: "3m123456789ab",
            uri: "at://did:plc:test/app.bsky.feed.post/3m123456789ab",
            cid: "bafyone"
        ),
        PublishedPostReference(
            rkey: "3m123456789ac",
            uri: "at://did:plc:test/app.bsky.feed.post/3m123456789ac",
            cid: "bafytwo"
        ),
    ]

    let roundTrip = try JSONDecoder().decode(
        SkejScheduleRecord.self,
        from: JSONEncoder().encode(modern)
    )
    #expect(roundTrip.publishedPosts.map(\.rkey) == ["3m123456789ab", "3m123456789ac"])
    #expect(roundTrip.publishRkey == legacy.publishRkey)
    #expect(roundTrip.publishedUri == legacy.publishedUri)
    #expect(roundTrip.publishedCid == legacy.publishedCid)
}

private func loadSocialMarkdownFixture() throws -> SocialMarkdownFixture {
    let fixtureURL = try #require(Bundle.module.url(forResource: "social-markdown", withExtension: "json", subdirectory: "Fixtures"))
    return try JSONDecoder().decode(
        SocialMarkdownFixture.self,
        from: Data(contentsOf: fixtureURL)
    )
}

private func goldenFacet(_ value: JSONValue) -> SocialMarkdownFixture.Facet? {
    guard case let .object(object) = value,
          case let .object(index)? = object["index"],
          case let .number(byteStart)? = index["byteStart"],
          case let .number(byteEnd)? = index["byteEnd"],
          case let .array(features)? = object["features"],
          case let .object(feature)? = features.first,
          case let .string(type)? = feature["$type"]
    else {
        return nil
    }

    if type == "app.bsky.richtext.facet#link",
       case let .string(uri)? = feature["uri"] {
        return .init(
            byteStart: Int(byteStart),
            byteEnd: Int(byteEnd),
            type: "link",
            value: uri
        )
    }
    if type == "app.bsky.richtext.facet#tag",
       case let .string(tag)? = feature["tag"] {
        return .init(
            byteStart: Int(byteStart),
            byteEnd: Int(byteEnd),
            type: "tag",
            value: tag
        )
    }
    if type == "app.bsky.richtext.facet#mention",
       case let .string(did)? = feature["did"] {
        return .init(
            byteStart: Int(byteStart),
            byteEnd: Int(byteEnd),
            type: "mention",
            value: did
        )
    }
    return nil
}
