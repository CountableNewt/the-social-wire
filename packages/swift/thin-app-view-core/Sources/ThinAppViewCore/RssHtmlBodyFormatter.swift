import Foundation

public enum RssHtmlBodyFormatter {
  public static func htmlBody(contentHTML: String?, summary: String?) -> String {
    if let html = contentHTML?.trimmingCharacters(in: .whitespacesAndNewlines),
       !html.isEmpty
    {
      let cleaned = stripNoise(html)
      return looksLikeHTML(cleaned) ? cleaned : plainTextToHtml(cleaned)
    }
    if let snippet = summary?.trimmingCharacters(in: .whitespacesAndNewlines),
       !snippet.isEmpty
    {
      let cleaned = stripNoise(snippet)
      return looksLikeHTML(cleaned) ? cleaned : plainTextToHtml(cleaned)
    }
    return "<p></p>"
  }

  private static func looksLikeHTML(_ text: String) -> Bool {
    text.range(of: #"<[a-zA-Z][^>]*>"#, options: .regularExpression) != nil
  }

  private static func plainTextToHtml(_ text: String) -> String {
    let normalized = text
      .replacingOccurrences(of: "\r\n", with: "\n")
      .replacingOccurrences(of: "\r", with: "\n")
      .trimmingCharacters(in: .whitespacesAndNewlines)
    guard !normalized.isEmpty else { return "<p></p>" }

    return normalized
      .components(separatedBy: paragraphBreakRegex)
      .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
      .filter { !$0.isEmpty }
      .map { paragraph in
        let lines = paragraph
          .components(separatedBy: "\n")
          .map { escapeHtml($0.trimmingCharacters(in: .whitespacesAndNewlines)) }
          .filter { !$0.isEmpty }
        return lines.isEmpty ? "" : "<p>\(lines.joined(separator: "<br />"))</p>"
      }
      .filter { !$0.isEmpty }
      .joined()
  }

  private static func stripNoise(_ text: String) -> String {
    text
      .replacingOccurrences(
        of: #"<script\b[^>]*>[\s\S]*?</script>"#,
        with: "",
        options: .regularExpression
      )
      .replacingOccurrences(
        of: #"<style\b[^>]*>[\s\S]*?</style>"#,
        with: "",
        options: .regularExpression
      )
      .replacingOccurrences(
        of: #"\bwindow\.[A-Za-z_$][\w$]*\s*=\s*\{[\s\S]*?\};?"#,
        with: "",
        options: .regularExpression
      )
      .replacingOccurrences(
        of: #"\b(?:const|let|var)\s+[A-Za-z_$][\w$]*\s*=\s*\{[\s\S]*?\};?"#,
        with: "",
        options: .regularExpression
      )
      .trimmingCharacters(in: .whitespacesAndNewlines)
  }

  private static let paragraphBreakRegex = try! NSRegularExpression(pattern: #"\n{2,}"#)

  private static func escapeHtml(_ text: String) -> String {
    text
      .replacingOccurrences(of: "&", with: "&amp;")
      .replacingOccurrences(of: "<", with: "&lt;")
      .replacingOccurrences(of: ">", with: "&gt;")
      .replacingOccurrences(of: "\"", with: "&quot;")
  }
}

private extension String {
  func components(separatedBy regex: NSRegularExpression) -> [String] {
    let range = NSRange(startIndex..<endIndex, in: self)
    var parts: [String] = []
    var cursor = startIndex
    for match in regex.matches(in: self, range: range) {
      guard let matchRange = Range(match.range, in: self) else { continue }
      parts.append(String(self[cursor..<matchRange.lowerBound]))
      cursor = matchRange.upperBound
    }
    parts.append(String(self[cursor..<endIndex]))
    return parts
  }
}
