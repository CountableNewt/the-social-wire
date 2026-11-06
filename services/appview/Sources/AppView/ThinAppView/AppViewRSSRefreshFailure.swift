import Foundation
import ThinAppViewCore

/// Viewer-private source identity never appears in error descriptions or logs.
struct AppViewRSSRefreshFailure: Error, Sendable, CustomStringConvertible {
  let publicationId: String
  let host: String
  let category: String

  init(feedUrl: String, category: String = "rss_backoff") {
    self.publicationId = RssFeedIdentity.rssPublicationId(from: feedUrl)
    self.host = URL(string: feedUrl)?.host?.lowercased() ?? ""
    self.category = category
  }

  var description: String { "RSS source unavailable" }
}
