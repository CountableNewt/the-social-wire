import Foundation
import SwiftUI

enum HTMLRenderer {
    static func prepareArticleBody(_ raw: String) -> String {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return "" }

        if let repaired = repairedEscapedHtmlWrapper(trimmed) {
            return repaired
        }

        if !trimmed.contains("<") {
            return plainTextParagraphs(trimmed)
        }

        return trimmed
    }

    static func wrappedHTML(_ html: String, colorScheme: ColorScheme) -> String {
        let body = prepareArticleBody(html)
        let palette = ReaderPalette(colorScheme: colorScheme)
        let darkOverrides = colorScheme == .dark
            ? """
          body, body *:not(a):not(img):not(video):not(svg):not(path) {
            color: \(palette.text) !important;
          }
          body a, body a * { color: \(palette.link) !important; }
          body pre, body code, body kbd, body samp {
            background: \(palette.codeBackground) !important;
          }
        """
            : ""

        return """
        <!DOCTYPE html>
        <html>
        <head>
        <meta charset="utf-8">
        <meta name="viewport" content="width=device-width, initial-scale=1">
        <meta name="color-scheme" content="light dark">
        <meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src https: data:; style-src 'unsafe-inline'; font-src data:;">
        <style>
          /* Mirrors the web reading view (Tailwind `prose prose-sm`): system font stack,
             ~1.7 line-height, the same heading scale, and full-measure body (no max-width). */
          :root { color-scheme: light dark; }
          body {
            font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", "Helvetica Neue", sans-serif;
            font-size: 16px;
            color: \(palette.text);
            background: transparent;
            line-height: 1.7142857;
            padding: 4px 16px 32px;
            margin: 0;
            max-width: none;
            overflow-wrap: break-word;
            -webkit-text-size-adjust: 100%;
          }
          h1, h2, h3, h4, h5, h6 {
            color: \(palette.text);
            font-weight: 600;
            line-height: 1.3;
            margin: 1.6em 0 0.6em;
          }
          h1 { font-size: 1.9em; line-height: 1.2; }
          h2 { font-size: 1.5em; }
          h3 { font-size: 1.25em; }
          h4 { font-size: 1.05em; }
          h1:first-child, h2:first-child, h3:first-child { margin-top: 0; }
          p, li, span, div, td, th, blockquote, figcaption, label {
            color: \(palette.text);
          }
          p { margin: 0 0 1.14em; }
          ul, ol { margin: 0 0 1.14em; padding-left: 1.6em; }
          li { margin: 0.35em 0; }
          a, a:visited { color: \(palette.link); text-decoration: underline; }
          img, video { max-width: 100%; height: auto; border-radius: 8px; margin: 1.14em 0; }
          figure { margin: 1.14em 0; }
          figcaption { font-size: 0.85em; color: \(palette.muted); text-align: center; }
          pre, code, kbd, samp {
            font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
            background: \(palette.codeBackground);
            color: \(palette.text);
            border-radius: 6px;
            font-size: 0.875em;
          }
          pre {
            overflow-x: auto;
            white-space: pre-wrap;
            padding: 12px 14px;
            margin: 1.14em 0;
          }
          code { padding: 0.1em 0.3em; }
          pre code { padding: 0; background: transparent; font-size: 1em; }
          blockquote {
            border-left: 3px solid \(palette.muted);
            margin: 1.14em 0;
            padding-left: 1em;
            color: \(palette.muted);
            font-style: italic;
          }
          table { border-collapse: collapse; width: 100%; margin: 1.14em 0; font-size: 0.9em; }
          th, td { border: 1px solid \(palette.border); padding: 6px 8px; }
          hr { border: none; border-top: 1px solid \(palette.border); margin: 1.7em 0; }
          \(darkOverrides)
        </style>
        </head>
        <body>\(body)</body>
        </html>
        """
    }

    private struct ReaderPalette {
        let text: String
        let link: String
        let muted: String
        let border: String
        let codeBackground: String

        init(colorScheme: ColorScheme) {
            switch colorScheme {
            case .dark:
                text = "#F5F5F7"
                link = "#6EB6FF"
                muted = "#98989D"
                border = "#3A3A3C"
                codeBackground = "#2C2C2E"
            default:
                text = "#1C1C1E"
                link = "#007AFF"
                muted = "#8E8E93"
                border = "#D1D1D6"
                codeBackground = "#F2F2F7"
            }
        }
    }

    private static func plainTextParagraphs(_ text: String) -> String {
        let paragraphs = text
            .components(separatedBy: "\n\n")
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
        guard !paragraphs.isEmpty else { return "<p></p>" }
        return paragraphs
            .map { paragraph in
                let lines = paragraph
                    .components(separatedBy: "\n")
                    .map(escapeHtml)
                    .joined(separator: "<br>")
                return "<p>\(lines)</p>"
            }
            .joined()
    }

    /// Repairs legacy RSS rows that stored HTML summaries as escaped markup inside a single `<p>`.
    private static func repairedEscapedHtmlWrapper(_ html: String) -> String? {
        guard html.hasPrefix("<p>"), html.hasSuffix("</p>") else { return nil }
        let innerStart = html.index(html.startIndex, offsetBy: 3)
        let innerEnd = html.index(html.endIndex, offsetBy: -4)
        guard innerStart < innerEnd else { return nil }
        let inner = String(html[innerStart ..< innerEnd])
        guard inner.contains("&lt;"), !inner.contains("<") else { return nil }
        return unescapeHtmlEntities(inner)
    }

    private static func escapeHtml(_ text: String) -> String {
        text
            .replacingOccurrences(of: "&", with: "&amp;")
            .replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;")
            .replacingOccurrences(of: "\"", with: "&quot;")
    }

    private static func unescapeHtmlEntities(_ text: String) -> String {
        text
            .replacingOccurrences(of: "&lt;", with: "<")
            .replacingOccurrences(of: "&gt;", with: ">")
            .replacingOccurrences(of: "&quot;", with: "\"")
            .replacingOccurrences(of: "&#39;", with: "'")
            .replacingOccurrences(of: "&apos;", with: "'")
            .replacingOccurrences(of: "&amp;", with: "&")
    }
}
