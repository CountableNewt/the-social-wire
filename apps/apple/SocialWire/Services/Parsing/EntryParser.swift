import Foundation

enum EntryParser {
    static func parseListItem(record: GenericRepoRecord) -> EntryListItem {
        let value = record.value.object ?? [:]
        let title = firstString(value, keys: ["title", "name", "headline"]) ?? "Untitled"
        let summary = firstString(value, keys: ["summary", "description", "excerpt"])
        let publishedAt = firstString(value, keys: ["publishedAt", "createdAt", "datePublished"]) ?? DateFormatters.string()
        return EntryListItem(
            entryId: record.uri,
            title: title,
            summary: summary,
            publishedAt: publishedAt,
            thumbnailUrl: thumbnail(from: value),
            thumbnailFallbackUrl: nil
        )
    }

    static func parseDetail(record: GenericRepoRecord) -> EntryDetail {
        let value = record.value.object ?? [:]
        let list = parseListItem(record: record)
        let content = firstString(value, keys: ["contentHtml", "html", "content", "body", "text"]) ?? list.summary ?? ""
        let original = firstString(value, keys: ["url", "uri", "originalUrl", "canonicalUrl"])
        let site = firstString(value, keys: ["site", "origin"])
        let path = firstString(value, keys: ["path"])
        let embed = original ?? embedURL(site: site, path: path)
        let strongRef = strongRef(from: value["bskyPostRef"])
        return EntryDetail(
            entryId: record.uri,
            title: list.title,
            publishedAt: list.publishedAt,
            contentHtml: content,
            originalUrl: original,
            embedUrl: embed,
            bskyPostUri: strongRef?.uri,
            bskyPostCid: strongRef?.cid
        )
    }

    static func recordBelongsToPublication(_ value: JSONValue, publicationAtUri: String) -> Bool {
        guard let object = value.object else { return false }
        return firstString(object, keys: ["publication", "publicationUri", "publicationId"]) == publicationAtUri
    }

    private static func firstString(_ object: [String: JSONValue], keys: [String]) -> String? {
        for key in keys {
            if let value = object[key]?.string?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty {
                return value
            }
        }
        return nil
    }

    private static func thumbnail(from object: [String: JSONValue]) -> String? {
        if let direct = firstString(object, keys: ["thumbnail", "thumbnailUrl", "image", "coverImage"]) {
            return direct
        }
        if let imageObject = object["image"]?.object {
            return firstString(imageObject, keys: ["url", "ref"])
        }
        return nil
    }

    private static func embedURL(site: String?, path: String?) -> String? {
        guard let site else { return nil }
        if let path, let url = URL(string: path, relativeTo: URL(string: site)) {
            return url.absoluteString
        }
        return site
    }

    private static func strongRef(from value: JSONValue?) -> StrongRef? {
        guard let object = value?.object,
              let uri = object["uri"]?.string,
              let cid = object["cid"]?.string
        else { return nil }
        return StrongRef(uri: uri, cid: cid)
    }
}
