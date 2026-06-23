import Foundation

/// Lenient parsers for L@tr PDS records — skip malformed rows instead of failing the whole list.
enum LatrRecordParsing {
    static func parseItem(_ record: GenericRepoRecord) -> RepoRecord<LatrSavedItemRecord>? {
        guard let object = record.value.object else { return nil }
        guard let subjectUri = trimmedString(object, keys: ["subjectUri", "subjectURI"]),
              !subjectUri.isEmpty
        else { return nil }

        let savedAt = trimmedString(object, keys: ["savedAt", "createdAt"]) ?? DateFormatters.string()
        let type = trimmedString(object, keys: ["$type"]) ?? PDSRecordService.latrSavedItem

        let value = LatrSavedItemRecord(
            type: type,
            subjectUri: subjectUri,
            savedAt: savedAt,
            state: trimmedString(object, keys: ["state"]),
            tags: stringArray(object["tags"]),
            note: trimmedString(object, keys: ["note"]),
            lastOpenedAt: trimmedString(object, keys: ["lastOpenedAt"]),
            linkedWebUrl: trimmedString(object, keys: ["linkedWebUrl", "linkedWebURL"]),
            previewTitle: trimmedString(object, keys: ["previewTitle"]),
            previewExcerpt: trimmedString(object, keys: ["previewExcerpt"]),
            previewSite: trimmedString(object, keys: ["previewSite"]),
            previewImage: trimmedString(object, keys: ["previewImage"]),
            previewAuthor: trimmedString(object, keys: ["previewAuthor"])
        )
        return RepoRecord(uri: record.uri, cid: record.cid, value: value)
    }

    static func parseExternal(_ record: GenericRepoRecord) -> RepoRecord<LatrSavedExternalRecord>? {
        guard let object = record.value.object else { return nil }
        guard let url = trimmedString(object, keys: ["url"]),
              !url.isEmpty
        else { return nil }

        let normalizedUrl = trimmedString(object, keys: ["normalizedUrl", "normalizedURL"]) ?? url
        let fingerprint = trimmedString(object, keys: ["fingerprint"]) ?? ""
        let createdAt = trimmedString(object, keys: ["createdAt"]) ?? DateFormatters.string()
        let type = trimmedString(object, keys: ["$type"]) ?? PDSRecordService.latrSavedExternal

        let value = LatrSavedExternalRecord(
            type: type,
            url: url,
            normalizedUrl: normalizedUrl,
            fingerprint: fingerprint,
            createdAt: createdAt,
            title: trimmedString(object, keys: ["title"]),
            excerpt: trimmedString(object, keys: ["excerpt"]),
            site: trimmedString(object, keys: ["site"]),
            image: trimmedString(object, keys: ["image"]),
            language: trimmedString(object, keys: ["language"]),
            publishedAt: trimmedString(object, keys: ["publishedAt"]),
            author: trimmedString(object, keys: ["author"])
        )
        return RepoRecord(uri: record.uri, cid: record.cid, value: value)
    }

    private static func trimmedString(_ object: [String: JSONValue], keys: [String]) -> String? {
        for key in keys {
            guard let raw = object[key]?.string else { continue }
            let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmed.isEmpty { return trimmed }
        }
        return nil
    }

    private static func stringArray(_ value: JSONValue?) -> [String]? {
        guard let items = value?.array else { return nil }
        let strings = items.compactMap(\.string).filter { !$0.isEmpty }
        return strings.isEmpty ? nil : strings
    }
}
