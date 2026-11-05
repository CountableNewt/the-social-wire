import Foundation

struct AnalyticsSubject: Sendable {
  let did: String
  let collection: String
  let rkey: String

  init(_ uri: String, publication: Bool = false) throws {
    guard uri.hasPrefix("at://"), uri.utf8.count <= 2048,
      !uri.contains(where: { "?#@\\ \r\n\t".contains($0) }) else { throw AnalyticsError.invalid }
    let pieces = uri.dropFirst(5).split(separator: "/", omittingEmptySubsequences: false).map(String.init)
    guard pieces.count == 3, pieces[0].hasPrefix("did:"),
      pieces[0].split(separator: ":", maxSplits: 2, omittingEmptySubsequences: false).count == 3,
      pieces[0].split(separator: ":", maxSplits: 2, omittingEmptySubsequences: false).last?.isEmpty == false,
      !pieces[2].isEmpty, pieces[2].utf8.count <= 512, !pieces[2].contains("%"),
      pieces[2] != ".", pieces[2] != "..",
      (publication ? pieces[1] == "site.standard.publication" : ["site.standard.document", "app.bsky.feed.post"].contains(pieces[1])) else {
      throw AnalyticsError.invalid
    }
    did = pieces[0]; collection = pieces[1]; rkey = pieces[2]
  }
}
