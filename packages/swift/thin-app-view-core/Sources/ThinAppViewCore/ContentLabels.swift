import Foundation

/// Recognized author-declared metadata only; labels never change feed membership.
public enum ContentLabels {
  private static let supported = ["ai-generated", "sexual"]

  public static func fromRecord(_ record: [String: Any]) -> [String]? {
    guard record["$type"] as? String == "site.standard.document",
      let labels = record["labels"] as? [String: Any],
      labels["$type"] as? String == "com.atproto.label.defs#selfLabels",
      let values = labels["values"] as? [[String: Any]]
    else { return nil }
    return normalize(values.compactMap { $0["val"] as? String })
  }

  static func decodeJSON(_ json: String?) -> [String]? {
    guard let json, let data = json.data(using: .utf8),
      let values = try? JSONDecoder().decode([String].self, from: data)
    else { return nil }
    return normalize(values)
  }

  private static func normalize(_ values: [String]) -> [String]? {
    let normalized = supported.filter(Set(values).contains)
    return normalized.isEmpty ? nil : normalized
  }
}
