import Foundation

public struct SocialMarkdownCompilation: Equatable, Sendable {
    public let text: String
    public let facets: [JSONValue]

    public init(text: String, facets: [JSONValue]) {
        self.text = text
        self.facets = facets
    }
}

/// Compiles the deliberately small social-Markdown profile accepted by Skej into
/// an `app.bsky.feed.post` text projection and UTF-8 link facets.
///
/// Inline and fenced code markers remain available for clients that render them
/// locally. This is intentionally not a general HTML or CommonMark renderer;
/// unsupported and malformed constructs remain literal so input is never lost.
public enum SocialMarkdownCompiler {
    public static func compile(
        _ markdown: String,
        resolvedMentions: [ResolvedMention] = []
    ) -> SocialMarkdownCompilation {
        var buffer = CompilationBuffer()
        let normalized = markdown
            .replacingOccurrences(of: "\r\n", with: "\n")
            .replacingOccurrences(of: "\r", with: "\n")
        let lines = normalized.split(separator: "\n", omittingEmptySubsequences: false)
        var fencedDelimiter: String?

        for (index, lineSlice) in lines.enumerated() {
            let line = String(lineSlice)
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            let delimiter = fenceDelimiter(in: trimmed)

            if let activeDelimiter = fencedDelimiter {
                buffer.appendSuppressed(line)
                if delimiter == activeDelimiter {
                    fencedDelimiter = nil
                }
            } else if let delimiter {
                fencedDelimiter = delimiter
                buffer.appendSuppressed(line)
            } else if isExcludedBlock(line) {
                buffer.appendSuppressed(line)
            } else {
                let transformed = structuralPrefix(in: line)
                buffer.append(transformed.prefix)
                compileInline(transformed.body[...], into: &buffer)
            }

            if index < lines.count - 1 {
                buffer.append("\n")
            }
        }

        appendTagFacets(to: &buffer)
        appendMentionFacets(resolvedMentions, to: &buffer)
        buffer.sortFacets()
        return SocialMarkdownCompilation(text: buffer.text, facets: buffer.facets)
    }

    public static func project(_ source: PostSource) -> SocialMarkdownCompilation {
        let compilation = compile(source.text, resolvedMentions: source.mentions ?? [])
        guard let first = compilation.text.firstIndex(where: { !$0.isWhitespace }),
              let last = compilation.text.lastIndex(where: { !$0.isWhitespace })
        else {
            return SocialMarkdownCompilation(text: "", facets: [])
        }
        let end = compilation.text.index(after: last)
        let text = String(compilation.text[first..<end])
        let leadingBytes = compilation.text[..<first].utf8.count
        let finalBytes = text.utf8.count
        let facets: [JSONValue] = compilation.facets.compactMap { facet -> JSONValue? in
            guard let range = CompilationBuffer.byteRange(facet),
                  range.lowerBound >= leadingBytes,
                  range.upperBound <= leadingBytes + finalBytes,
                  case var .object(object) = facet
            else { return nil }
            object["index"] = .object([
                "byteStart": .number(Double(range.lowerBound - leadingBytes)),
                "byteEnd": .number(Double(range.upperBound - leadingBytes)),
            ])
            return .object(object)
        }
        return SocialMarkdownCompilation(text: text, facets: facets)
    }

    private static func compileInline(_ source: Substring, into buffer: inout CompilationBuffer) {
        var index = source.startIndex

        while index < source.endIndex {
            if source[index] == "\\", let escaped = source.index(index, offsetBy: 1, limitedBy: source.endIndex), escaped < source.endIndex,
               isEscapable(source[escaped]) {
                buffer.appendSuppressed(String(source[escaped]))
                index = source.index(after: escaped)
                continue
            }

            if source[index] == "<", let end = source[index...].firstIndex(of: ">") {
                buffer.appendSuppressed(String(source[index...end]))
                index = source.index(after: end)
                continue
            }

            if hasPrefix("![", at: index, in: source),
               let syntax = linkSyntax(startingAt: source.index(after: index), in: source) {
                buffer.appendSuppressed(String(source[index..<syntax.end]))
                index = syntax.end
                continue
            }

            if source[index] == "[", let syntax = linkSyntax(startingAt: index, in: source) {
                let destination = String(source[syntax.destination]).trimmingCharacters(in: .whitespaces)
                if !source[syntax.label].isEmpty, isSafeHTTPURL(destination) {
                    let start = buffer.utf8Count
                    compileInline(source[syntax.label], into: &buffer)
                    let end = buffer.utf8Count
                    if start < end {
                        buffer.removeFacets(overlapping: start..<end)
                        buffer.appendLinkFacet(byteStart: start, byteEnd: end, uri: destination)
                    }
                } else {
                    buffer.appendSuppressed(String(source[index..<syntax.end]))
                }
                index = syntax.end
                continue
            }

            if source[index] == "`", let closing = closingMarker("`", after: index, in: source) {
                let contentStart = source.index(after: index)
                if contentStart < closing {
                    buffer.appendSuppressed(String(source[index...closing]))
                    index = source.index(after: closing)
                    continue
                }
            }

            if let marker = emphasisMarker(at: index, in: source),
               let closing = closingMarker(marker, after: index, in: source) {
                let contentStart = source.index(index, offsetBy: marker.count)
                if contentStart < closing {
                    compileInline(source[contentStart..<closing], into: &buffer)
                    index = source.index(closing, offsetBy: marker.count)
                    continue
                }
            }

            if hasCaseInsensitivePrefix("https://", at: index, in: source) ||
                hasCaseInsensitivePrefix("http://", at: index, in: source) {
                let candidateEnd = bareURLCandidateEnd(startingAt: index, in: source)
                let candidate = String(source[index..<candidateEnd])
                let link = trimmedBareURL(candidate)
                if isSafeHTTPURL(link) {
                    let start = buffer.utf8Count
                    buffer.append(candidate)
                    buffer.appendLinkFacet(
                        byteStart: start,
                        byteEnd: start + link.utf8.count,
                        uri: link
                    )
                    index = candidateEnd
                    continue
                }
            }

            buffer.append(String(source[index]))
            index = source.index(after: index)
        }
    }

    private static func structuralPrefix(in line: String) -> (prefix: String, body: String) {
        var body = line[...]
        var indentation = ""
        while body.first == " " || body.first == "\t" {
            indentation.append(body.removeFirst())
        }

        var quotePrefix = ""
        while body.first == ">" {
            body.removeFirst()
            if body.first == " " {
                body.removeFirst()
            }
            quotePrefix += "› "
        }

        if body.count >= 2,
           let marker = body.first,
           ["-", "+", "*"].contains(marker),
           body[body.index(after: body.startIndex)] == " " {
            body.removeFirst(2)
            return (indentation + quotePrefix + "• ", String(body))
        }

        var digitEnd = body.startIndex
        while digitEnd < body.endIndex, body[digitEnd].isNumber {
            digitEnd = body.index(after: digitEnd)
        }
        if digitEnd > body.startIndex, digitEnd < body.endIndex,
           body[digitEnd] == "." || body[digitEnd] == ")" {
            let space = body.index(after: digitEnd)
            if space < body.endIndex, body[space] == " " {
                let number = body[body.startIndex..<digitEnd]
                let content = body[body.index(after: space)...]
                return (indentation + quotePrefix + number + ". ", String(content))
            }
        }

        return (indentation + quotePrefix, String(body))
    }

    private static func fenceDelimiter(in trimmedLine: String) -> String? {
        if trimmedLine.hasPrefix("```") { return "```" }
        if trimmedLine.hasPrefix("~~~") { return "~~~" }
        return nil
    }

    private static func isExcludedBlock(_ line: String) -> Bool {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        let hashes = trimmed.prefix(while: { $0 == "#" })
        if (1 ... 6).contains(hashes.count),
           trimmed.dropFirst(hashes.count).first?.isWhitespace == true {
            return true
        }
        if trimmed.hasPrefix("|") {
            return true
        }
        if let marker = trimmed.first, ["-", "+", "*"].contains(marker) {
            let remainder = trimmed.dropFirst().drop(while: { $0.isWhitespace })
            if remainder.hasPrefix("[ ] ") || remainder.hasPrefix("[x] ") || remainder.hasPrefix("[X] ") {
                return true
            }
        }
        return trimmed == "---" || trimmed == "***" || trimmed == "___"
    }

    private static func emphasisMarker(at index: Substring.Index, in source: Substring) -> String? {
        for marker in ["**", "__", "~~", "*", "_"] where hasPrefix(marker, at: index, in: source) {
            let contentStart = source.index(index, offsetBy: marker.count)
            guard contentStart < source.endIndex, !source[contentStart].isWhitespace else { continue }
            if marker.contains("_"), index > source.startIndex,
               isAlphanumeric(source[source.index(before: index)]) {
                continue
            }
            return marker
        }
        return nil
    }

    private static func closingMarker(
        _ marker: String,
        after opening: Substring.Index,
        in source: Substring
    ) -> Substring.Index? {
        var index = source.index(opening, offsetBy: marker.count)
        while index < source.endIndex {
            if source[index] == "\\" {
                index = source.index(after: index)
                if index < source.endIndex { index = source.index(after: index) }
                continue
            }
            if hasPrefix(marker, at: index, in: source) {
                let before = source.index(before: index)
                let after = source.index(index, offsetBy: marker.count)
                if !source[before].isWhitespace,
                   !(marker.contains("_") && after < source.endIndex && isAlphanumeric(source[after])) {
                    return index
                }
            }
            index = source.index(after: index)
        }
        return nil
    }

    private static func linkSyntax(startingAt opening: Substring.Index, in source: Substring) -> LinkSyntax? {
        guard source[opening] == "[" else { return nil }
        var closeBracket = source.index(after: opening)
        while closeBracket < source.endIndex {
            if source[closeBracket] == "\\" {
                closeBracket = source.index(after: closeBracket)
                if closeBracket < source.endIndex { closeBracket = source.index(after: closeBracket) }
                continue
            }
            if source[closeBracket] == "]" { break }
            closeBracket = source.index(after: closeBracket)
        }
        guard closeBracket < source.endIndex else { return nil }
        let openParen = source.index(after: closeBracket)
        guard openParen < source.endIndex, source[openParen] == "(" else { return nil }

        var depth = 1
        var cursor = source.index(after: openParen)
        while cursor < source.endIndex {
            if source[cursor] == "\\" {
                cursor = source.index(after: cursor)
                if cursor < source.endIndex { cursor = source.index(after: cursor) }
                continue
            }
            if source[cursor] == "(" { depth += 1 }
            if source[cursor] == ")" {
                depth -= 1
                if depth == 0 {
                    return LinkSyntax(
                        label: source.index(after: opening)..<closeBracket,
                        destination: source.index(after: openParen)..<cursor,
                        end: source.index(after: cursor)
                    )
                }
            }
            cursor = source.index(after: cursor)
        }
        return nil
    }

    private static func bareURLCandidateEnd(startingAt start: Substring.Index, in source: Substring) -> Substring.Index {
        var index = start
        while index < source.endIndex {
            let character = source[index]
            if character.isWhitespace || ["<", ">", "\"", "'"].contains(character) {
                break
            }
            index = source.index(after: index)
        }
        return index
    }

    private static func trimmedBareURL(_ candidate: String) -> String {
        var result = candidate
        while let last = result.last, ".,!?;:".contains(last) {
            result.removeLast()
        }
        for (opening, closing) in [("(", ")"), ("[", "]"), ("{", "}")] {
            while result.last == Character(closing),
                  result.filter({ $0 == Character(closing) }).count > result.filter({ $0 == Character(opening) }).count {
                result.removeLast()
            }
        }
        return result
    }

    private static func isSafeHTTPURL(_ value: String) -> Bool {
        guard !value.contains(where: \.isWhitespace),
              let components = URLComponents(string: value),
              let scheme = components.scheme?.lowercased(),
              scheme == "http" || scheme == "https",
              components.host?.isEmpty == false
        else {
            return false
        }
        return true
    }

    private static func appendTagFacets(to buffer: inout CompilationBuffer) {
        let expression = try! NSRegularExpression(
            pattern: #"(?:^|\s)([#\uFF03](?!\uFE0F)[^\s\u00AD\u2060\u200A\u200B\u200C\u200D\u20E2]*[^\d\s\p{P}\u00AD\u2060\u200A\u200B\u200C\u200D\u20E2]+[^\s\u00AD\u2060\u200A\u200B\u200C\u200D\u20E2]*)"#
        )
        let fullRange = NSRange(buffer.text.startIndex..<buffer.text.endIndex, in: buffer.text)
        let occupied = buffer.facets.compactMap(CompilationBuffer.byteRange) + buffer.suppressedRanges

        for match in expression.matches(in: buffer.text, range: fullRange) {
            guard let range = Range(match.range(at: 1), in: buffer.text) else { continue }
            let bodyStart = buffer.text.index(after: range.lowerBound)
            var body = String(buffer.text[bodyStart..<range.upperBound])
            while let last = body.last, last.isPunctuation {
                body.removeLast()
            }
            guard !body.isEmpty, body.count <= 64 else { continue }

            let byteStart = buffer.text[..<range.lowerBound].utf8.count
            let markerWidth = buffer.text[range.lowerBound..<bodyStart].utf8.count
            let byteEnd = byteStart + markerWidth + body.utf8.count
            let byteRange = byteStart..<byteEnd
            guard !occupied.contains(where: { $0.overlaps(byteRange) }) else { continue }
            buffer.appendTagFacet(byteStart: byteStart, byteEnd: byteEnd, tag: body)
        }
    }

    private static func appendMentionFacets(
        _ mentions: [ResolvedMention],
        to buffer: inout CompilationBuffer
    ) {
        let resolved = Dictionary(
            mentions.map { mention in
                (mention.handle.trimmingPrefix("@").lowercased(), mention.did)
            },
            uniquingKeysWith: { first, _ in first }
        )
        guard !resolved.isEmpty else { return }
        let expression = try! NSRegularExpression(
            pattern: #"(?:^|[\s\(\[\{\:>\"'])(@[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?)"#,
            options: [.caseInsensitive]
        )
        let fullRange = NSRange(buffer.text.startIndex..<buffer.text.endIndex, in: buffer.text)
        for match in expression.matches(in: buffer.text, range: fullRange) {
            guard let range = Range(match.range(at: 1), in: buffer.text) else { continue }
            let next = range.upperBound < buffer.text.endIndex ? buffer.text[range.upperBound] : nil
            if let next, next.isLetter || next.isNumber || ".-".contains(next) { continue }
            let display = buffer.text[range]
            let handle = display.dropFirst().lowercased()
            guard let did = resolved[handle], did.hasPrefix("did:") else { continue }
            let byteStart = buffer.text[..<range.lowerBound].utf8.count
            let byteEnd = byteStart + display.utf8.count
            let byteRange = byteStart..<byteEnd
            let occupied = buffer.facets.compactMap(CompilationBuffer.byteRange) + buffer.suppressedRanges
            guard !occupied.contains(where: { $0.overlaps(byteRange) }) else { continue }
            buffer.appendMentionFacet(byteStart: byteStart, byteEnd: byteEnd, did: did)
        }
    }

    private static func isEscapable(_ character: Character) -> Bool {
        "\\`*_{}[]()#+-.!~>".contains(character)
    }

    private static func isAlphanumeric(_ character: Character) -> Bool {
        character.isLetter || character.isNumber
    }

    private static func hasPrefix(_ prefix: String, at index: Substring.Index, in source: Substring) -> Bool {
        source[index...].hasPrefix(prefix)
    }

    private static func hasCaseInsensitivePrefix(
        _ prefix: String,
        at index: Substring.Index,
        in source: Substring
    ) -> Bool {
        source[index...].prefix(prefix.count).lowercased() == prefix
    }
}

private extension String {
    func trimmingPrefix(_ prefix: Character) -> String {
        first == prefix ? String(dropFirst()) : self
    }
}

private struct LinkSyntax {
    let label: Range<Substring.Index>
    let destination: Range<Substring.Index>
    let end: Substring.Index
}

private struct CompilationBuffer {
    var text = ""
    var facets: [JSONValue] = []
    var suppressedRanges: [Range<Int>] = []

    var utf8Count: Int { text.utf8.count }

    mutating func append(_ value: String) {
        text += value
    }

    mutating func appendSuppressed(_ value: String) {
        let start = utf8Count
        text += value
        let end = utf8Count
        if start < end {
            suppressedRanges.append(start..<end)
        }
    }

    mutating func appendLinkFacet(byteStart: Int, byteEnd: Int, uri: String) {
        facets.append(.object([
            "index": .object([
                "byteStart": .number(Double(byteStart)),
                "byteEnd": .number(Double(byteEnd)),
            ]),
            "features": .array([
                .object([
                    "$type": .string("app.bsky.richtext.facet#link"),
                    "uri": .string(uri),
                ]),
            ]),
        ]))
    }

    mutating func appendTagFacet(byteStart: Int, byteEnd: Int, tag: String) {
        facets.append(.object([
            "index": .object([
                "byteStart": .number(Double(byteStart)),
                "byteEnd": .number(Double(byteEnd)),
            ]),
            "features": .array([
                .object([
                    "$type": .string("app.bsky.richtext.facet#tag"),
                    "tag": .string(tag),
                ]),
            ]),
        ]))
    }

    mutating func appendMentionFacet(byteStart: Int, byteEnd: Int, did: String) {
        facets.append(.object([
            "index": .object([
                "byteStart": .number(Double(byteStart)),
                "byteEnd": .number(Double(byteEnd)),
            ]),
            "features": .array([
                .object([
                    "$type": .string("app.bsky.richtext.facet#mention"),
                    "did": .string(did),
                ]),
            ]),
        ]))
    }

    mutating func removeFacets(overlapping range: Range<Int>) {
        facets.removeAll { facet in
            guard let existing = Self.byteRange(facet) else { return true }
            return existing.overlaps(range)
        }
    }

    mutating func sortFacets() {
        facets.sort {
            (Self.byteRange($0)?.lowerBound ?? Int.max) <
                (Self.byteRange($1)?.lowerBound ?? Int.max)
        }
    }

    static func byteRange(_ facet: JSONValue) -> Range<Int>? {
        guard case let .object(object) = facet,
              case let .object(index)? = object["index"],
              case let .number(start)? = index["byteStart"],
              case let .number(end)? = index["byteEnd"]
        else {
            return nil
        }
        return Int(start)..<Int(end)
    }
}
