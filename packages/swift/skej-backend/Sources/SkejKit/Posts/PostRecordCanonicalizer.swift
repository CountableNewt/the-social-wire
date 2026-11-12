import Foundation

public enum PostRecordValidationError: Error, Equatable, Sendable {
    case invalidFacet
}

public enum PostRecordCanonicalizer {
    private static let urlExpression = try! NSRegularExpression(
        pattern: #"https?://[^\s<>"']+"#,
        options: [.caseInsensitive]
    )
    // Mirrors the tag detection in @atproto/api: a hashtag starts the text or follows
    // whitespace, must hold at least one character that is not a digit, punctuation, or
    // an invisible separator, and its facet range covers the leading "#".
    private static let tagExpression = try! NSRegularExpression(
        pattern: #"(?:^|\s)([#\uFF03](?!\uFE0F)[^\s\u00AD\u2060\u200A\u200B\u200C\u200D\u20E2]*[^\d\s\p{P}\u00AD\u2060\u200A\u200B\u200C\u200D\u20E2]+[^\s\u00AD\u2060\u200A\u200B\u200C\u200D\u20E2]*)"#
    )
    private static let maxTagLength = 64
    private static let simpleTrailingPunctuation = CharacterSet(charactersIn: ".,!?;:")
    private static let closingPairs: [(Character, Character)] = [
        ("(", ")"),
        ("[", "]"),
        ("{", "}"),
    ]

    public static func canonicalize(_ plan: PostPlan) -> PostPlan {
        let compiled = plan.source.map(SocialMarkdownCompiler.project)
        let text = compiled?.text ?? plan.text
        return PostPlan(
            text: text,
            source: plan.source,
            publishRkey: plan.publishRkey,
            facets: facets(
                in: text,
                preserving: compiled == nil ? plan.facets : nil,
                including: compiled?.facets ?? [],
                detectTextFacets: compiled == nil && plan.facets == nil
            ),
            reply: plan.reply,
            embed: normalizeEmbed(plan.embed),
            langs: plan.langs,
            labels: plan.labels,
            tags: plan.tags,
            unknownFields: plan.unknownFields
        )
    }

    public static func canonicalizeFeedPost(_ value: JSONValue) -> JSONValue {
        guard case var .object(post) = value,
              case let .string(text)? = post["text"]
        else {
            return value
        }

        let hasExplicitFacets = post["facets"] != nil
        let existingFacets: [JSONValue]?
        if case let .array(facets)? = post["facets"] {
            existingFacets = facets
        } else {
            existingFacets = nil
        }
        if let facets = facets(
            in: text,
            preserving: existingFacets,
            detectTextFacets: !hasExplicitFacets
        ) {
            post["facets"] = .array(facets)
        } else {
            post.removeValue(forKey: "facets")
        }
        if let embed = normalizeEmbed(post["embed"]) {
            post["embed"] = embed
        }
        return .object(post)
    }

    public static func validateExplicitFacets(in plan: PostPlan) throws {
        guard let facets = plan.facets else { return }
        try validate(facets: facets, in: plan.text)
    }

    public static func validateExplicitFacets(inFeedPost value: JSONValue) throws {
        guard case let .object(post) = value,
              let explicitFacets = post["facets"]
        else { return }
        guard case let .string(text)? = post["text"],
              case let .array(facets) = explicitFacets
        else {
            throw PostRecordValidationError.invalidFacet
        }
        try validate(facets: facets, in: text)
    }

    public static func feedPostValue(
        _ plan: PostPlan,
        createdAt: String
    ) throws -> [String: JSONValue] {
        let canonical = canonicalize(plan)
        var post: [String: JSONValue] = [
            "$type": .string("app.bsky.feed.post"),
            "text": .string(canonical.text),
            "createdAt": .string(createdAt),
        ]
        if let facets = canonical.facets {
            post["facets"] = try facets.skejJSONValue()
        }
        if let reply = canonical.reply {
            post["reply"] = try reply.skejJSONValue()
        }
        if let embed = canonical.embed {
            post["embed"] = try embed.skejJSONValue()
        }
        if let langs = canonical.langs, !langs.isEmpty {
            post["langs"] = .array(langs.map { .string($0) })
        }
        if let labels = canonical.labels, !labels.isEmpty {
            post["labels"] = .object([
                "$type": .string("com.atproto.label.defs#selfLabels"),
                "values": .array(labels.map { .object(["val": .string($0)]) }),
            ])
        }
        if let tags = canonical.tags, !tags.isEmpty {
            post["tags"] = .array(tags.map { .string($0) })
        }
        return post
    }

    public static func facets(
        in text: String,
        preserving existingFacets: [JSONValue]? = nil,
        including authoredFacets: [JSONValue] = [],
        detectTextFacets: Bool = true
    ) -> [JSONValue]? {
        let shouldDetectTextFacets = detectTextFacets && existingFacets == nil
        var preservedRanges: [Range<Int>] = []
        var facets: [JSONValue] = []
        for facet in authoredFacets.sorted(by: {
            (facetByteRange($0)?.lowerBound ?? Int.max) <
                (facetByteRange($1)?.lowerBound ?? Int.max)
        }) {
            guard let range = facetByteRange(facet),
                  isValidFacetRange(range, in: text),
                  facetIsValidNativeFacet(facet, range: range, in: text),
                  !preservedRanges.contains(where: { $0.overlaps(range) })
            else { continue }
            facets.append(facet)
            preservedRanges.append(range)
        }
        for facet in (existingFacets ?? []).sorted(by: {
            (facetByteRange($0)?.lowerBound ?? Int.max) <
                (facetByteRange($1)?.lowerBound ?? Int.max)
        }) {
            guard let range = facetByteRange(facet),
                  isValidFacetRange(range, in: text),
                  facetIsValidNativeFacet(facet, range: range, in: text),
                  !preservedRanges.contains(where: { $0.overlaps(range) })
            else {
                continue
            }
            facets.append(facet)
            preservedRanges.append(range)
        }

        let links = (shouldDetectTextFacets ? detectedLinks(in: text) : []).filter { link in
            !preservedRanges.contains(where: { $0.overlaps(link.range) })
        }
        facets.append(contentsOf: links.map { link in
            facet(
                range: link.range,
                feature: [
                    "$type": .string("app.bsky.richtext.facet#link"),
                    "uri": .string(link.url),
                ]
            )
        })
        let occupiedRanges = preservedRanges + links.map(\.range)
        let tags = (shouldDetectTextFacets ? detectedTags(in: text) : []).filter { tag in
            !occupiedRanges.contains(where: { $0.overlaps(tag.range) })
        }
        facets.append(contentsOf: tags.map { tag in
            facet(
                range: tag.range,
                feature: [
                    "$type": .string("app.bsky.richtext.facet#tag"),
                    "tag": .string(tag.tag),
                ]
            )
        })
        facets.sort {
            (facetByteRange($0)?.lowerBound ?? Int.max) <
                (facetByteRange($1)?.lowerBound ?? Int.max)
        }
        return facets.isEmpty ? nil : facets
    }

    public static func normalizeEmbed(_ embed: JSONValue?) -> JSONValue? {
        guard case let .object(object) = embed else {
            return embed
        }
        if object["$type"] != nil {
            return embed
        }
        guard case let .object(external)? = object["external"],
              case let .string(uri)? = external["uri"]
        else {
            return embed
        }
        var normalizedExternal = external
        if normalizedExternal["title"] == nil {
            normalizedExternal["title"] = .string(URL(string: uri)?.host ?? uri)
        }
        if normalizedExternal["description"] == nil {
            normalizedExternal["description"] = .string("")
        }
        return .object([
            "$type": .string("app.bsky.embed.external"),
            "external": .object(normalizedExternal),
        ])
    }

    private static func detectedLinks(in text: String) -> [(url: String, range: Range<Int>)] {
        let fullRange = NSRange(text.startIndex..<text.endIndex, in: text)
        return urlExpression.matches(in: text, range: fullRange).compactMap { match in
            guard let matchRange = Range(match.range, in: text) else {
                return nil
            }
            let candidate = String(text[matchRange])
            let trimmed = trimTrailingPunctuation(candidate)
            guard !trimmed.isEmpty,
                  let components = URLComponents(string: trimmed),
                  let scheme = components.scheme?.lowercased(),
                  scheme == "http" || scheme == "https",
                  components.host != nil
            else {
                return nil
            }
            let end = text.index(matchRange.lowerBound, offsetBy: trimmed.count)
            let byteStart = text[..<matchRange.lowerBound].utf8.count
            let byteEnd = byteStart + text[matchRange.lowerBound..<end].utf8.count
            return (trimmed, byteStart..<byteEnd)
        }
    }

    private static func detectedTags(in text: String) -> [(tag: String, range: Range<Int>)] {
        let fullRange = NSRange(text.startIndex..<text.endIndex, in: text)
        return tagExpression.matches(in: text, range: fullRange).compactMap { match in
            guard let matchRange = Range(match.range(at: 1), in: text) else {
                return nil
            }
            // The capture keeps the leading "#", which the facet range covers but the
            // tag value omits.
            let body = trimTrailingTagPunctuation(text[matchRange].dropFirst())
            guard !body.isEmpty, body.count <= maxTagLength else {
                return nil
            }
            let byteStart = text[..<matchRange.lowerBound].utf8.count
            let byteEnd = byteStart + text[matchRange.lowerBound..<body.endIndex].utf8.count
            return (String(body), byteStart..<byteEnd)
        }
    }

    private static func trimTrailingTagPunctuation(_ value: Substring) -> Substring {
        var result = value
        while let last = result.last,
              last.unicodeScalars.allSatisfy(CharacterSet.punctuationCharacters.contains)
        {
            result = result.dropLast()
        }
        return result
    }

    private static func trimTrailingPunctuation(_ value: String) -> String {
        var result = value
        while let scalar = result.unicodeScalars.last,
              simpleTrailingPunctuation.contains(scalar)
        {
            result.removeLast()
        }
        var changed = true
        while changed {
            changed = false
            for (open, close) in closingPairs where result.last == close {
                let openCount = result.lazy.filter { $0 == open }.count
                let closeCount = result.lazy.filter { $0 == close }.count
                if closeCount > openCount {
                    result.removeLast()
                    changed = true
                }
            }
        }
        return result
    }

    private static func facet(range: Range<Int>, feature: [String: JSONValue]) -> JSONValue {
        .object([
            "index": .object([
                "byteStart": .number(Double(range.lowerBound)),
                "byteEnd": .number(Double(range.upperBound)),
            ]),
            "features": .array([.object(feature)]),
        ])
    }

    private static func facetIsValidMention(
        _ facet: JSONValue,
        range: Range<Int>,
        in text: String
    ) -> Bool {
        guard case let .object(object) = facet,
              case let .array(features)? = object["features"]
        else {
            return false
        }
        let utf8 = text.utf8
        let start = utf8.index(utf8.startIndex, offsetBy: range.lowerBound)
        let end = utf8.index(utf8.startIndex, offsetBy: range.upperBound)
        guard let textStart = start.samePosition(in: text),
              let textEnd = end.samePosition(in: text),
              text[textStart..<textEnd].first == "@",
              text.distance(from: textStart, to: textEnd) > 1
        else { return false }
        guard !features.isEmpty else { return false }
        return features.allSatisfy { feature in
            guard case let .object(object) = feature,
                  case let .string(type)? = object["$type"]
            else {
                return false
            }
            guard type == "app.bsky.richtext.facet#mention",
                  case let .string(did)? = object["did"]
            else { return false }
            return did.hasPrefix("did:") && did.count > 4
        }
    }

    private static func facetIsValidNativeFacet(
        _ facet: JSONValue,
        range: Range<Int>,
        in text: String
    ) -> Bool {
        guard case let .object(object) = facet,
              case let .array(features)? = object["features"],
              features.count == 1,
              case let .object(feature) = features[0],
              case let .string(type)? = feature["$type"]
        else { return false }
        if type == "app.bsky.richtext.facet#link",
           case let .string(uri)? = feature["uri"] {
            guard isSafeHTTPURL(uri),
                  !facetText(range, in: text).trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            else { return false }
            let overlappingDetectedLinks = detectedLinks(in: text).filter { $0.range.overlaps(range) }
            return overlappingDetectedLinks.allSatisfy { detected in
                detected.range == range
            }
        }
        if type == "app.bsky.richtext.facet#mention" {
            return facetIsValidMention(facet, range: range, in: text)
        }
        if type == "app.bsky.richtext.facet#tag",
           case let .string(tag)? = feature["tag"] {
            let displayed = facetText(range, in: text)
            return !tag.isEmpty && tag.count <= maxTagLength &&
                (displayed == "#\(tag)" || displayed == "＃\(tag)") &&
                detectedTags(in: text).contains { $0.range == range && $0.tag == tag }
        }
        return false
    }

    private static func validate(facets: [JSONValue], in text: String) throws {
        var ranges: [Range<Int>] = []
        for facet in facets {
            guard let range = facetByteRange(facet),
                  isValidFacetRange(range, in: text),
                  facetIsValidNativeFacet(facet, range: range, in: text),
                  !ranges.contains(where: { $0.overlaps(range) })
            else {
                throw PostRecordValidationError.invalidFacet
            }
            ranges.append(range)
        }
    }

    private static func facetText(_ range: Range<Int>, in text: String) -> String {
        let utf8 = text.utf8
        let start = utf8.index(utf8.startIndex, offsetBy: range.lowerBound)
        let end = utf8.index(utf8.startIndex, offsetBy: range.upperBound)
        guard let textStart = start.samePosition(in: text),
              let textEnd = end.samePosition(in: text)
        else { return "" }
        return String(text[textStart..<textEnd])
    }

    private static func isSafeHTTPURL(_ value: String) -> Bool {
        guard let components = URLComponents(string: value),
              let scheme = components.scheme?.lowercased(),
              scheme == "http" || scheme == "https",
              components.host?.isEmpty == false
        else { return false }
        return true
    }

    private static func isValidFacetRange(_ range: Range<Int>, in text: String) -> Bool {
        guard range.lowerBound >= 0,
              range.lowerBound < range.upperBound,
              range.upperBound <= text.utf8.count
        else { return false }
        let utf8 = text.utf8
        return utf8.index(utf8.startIndex, offsetBy: range.lowerBound).samePosition(in: text) != nil &&
            utf8.index(utf8.startIndex, offsetBy: range.upperBound).samePosition(in: text) != nil
    }

    private static func facetByteRange(_ facet: JSONValue) -> Range<Int>? {
        guard case let .object(object) = facet,
              case let .object(index)? = object["index"],
              case let .number(start)? = index["byteStart"],
              case let .number(end)? = index["byteEnd"]
        else {
            return nil
        }
        guard start.isFinite, end.isFinite,
              start.rounded(.towardZero) == start,
              end.rounded(.towardZero) == end
        else { return nil }
        return Int(start)..<Int(end)
    }
}
