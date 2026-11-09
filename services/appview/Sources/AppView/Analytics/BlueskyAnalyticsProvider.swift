import Foundation

struct BlueskyAnalyticsProvider: Sendable {
  let reader: any BlueskyAnalyticsReading

  func summary(author: String, days: Int, articles: [AnalyticsItem], at: Date) async -> BlueskyAnalyticsSummary {
    let since = at.addingTimeInterval(-Double(days) * 86400)
    async let profile = try? reader.profile(author: author)
    async let feed = collectFeed(author: author, since: since, until: at)
    async let shares = collectShares(articles: articles, since: since, until: at)
    var result = BlueskyAnalyticsSummary(sampledAt: at, days: days)
    result.profile = await profile ?? .init()
    let collected = await feed
    result.coverage.feedComplete = collected.complete
    result.coverage.feedAvailable = collected.available
    result.coverage.feedPostsExamined = collected.examined
    if collected.available {
      let posts = collected.posts.filter { $0.record.reply == nil }.count
      result.activity = .init(posts: posts, replies: collected.posts.count - posts, postsPerDay: Double(posts) / Double(days))
      result.topPosts = collected.posts.map(Self.present).sorted {
        let left = Self.score($0), right = Self.score($1)
        if left != right { return left > right }
        if $0.publishedAt != $1.publishedAt { return $0.publishedAt > $1.publishedAt }
        return $0.uri < $1.uri
      }.prefix(10).map { $0 }
    }
    let discovered = await shares
    result.shares = discovered.items
    result.coverage.sharesAvailable = discovered.available
    result.coverage.articlesSearched = discovered.searched
    return result
  }

  private func collectFeed(author: String, since: Date, until: Date) async -> (posts: [BlueskyAnalyticsFeed.Post], complete: Bool, available: Bool, examined: Int) {
    var posts: [BlueskyAnalyticsFeed.Post] = []
    var seen: Set<String> = []
    var cursors: Set<String> = []
    var cursor: String?
    var examined = 0
    for _ in 0..<3 {
      do {
        try Task.checkCancellation()
        let page = try await reader.feed(author: author, cursor: cursor)
        guard page.feed.count <= 100 else { throw AnalyticsError.providerUnavailable }
        examined += page.feed.count
        for row in page.feed {
          guard row.reason == nil, row.post.author.did == author,
            let subject = try? AnalyticsSubject(row.post.uri), subject.did == author,
            subject.collection == "app.bsky.feed.post", seen.insert(row.post.uri).inserted,
            let date = Self.date(row.post.record.createdAt) else { continue }
          if date >= since && date <= until { posts.append(row.post) }
        }
        cursor = page.cursor.flatMap { $0.isEmpty ? nil : $0 }
        if cursor == nil { return (posts, true, true, examined) }
        guard cursors.insert(cursor!).inserted else { return (posts, false, true, examined) }
      } catch { return (posts, false, !posts.isEmpty, examined) }
    }
    return (posts, false, true, examined)
  }

  private func collectShares(articles: [AnalyticsItem], since: Date, until: Date) async -> (items: [BlueskyAnalyticsShares], available: Bool, searched: Int) {
    var urls: Set<String> = []
    let eligible = articles.filter { $0.kind == "article" }.sorted { $0.publishedAt > $1.publishedAt }.filter {
      guard let url = URLComponents(string: $0.url), url.scheme == "https", url.host != nil,
        url.user == nil, url.password == nil else { return false }
      return urls.insert($0.url).inserted
    }.prefix(5)
    return await withTaskGroup(of: BlueskyAnalyticsShares?.self) { group in
      for article in eligible {
        group.addTask {
          do {
            let values = try await reader.shares(url: article.url, since: since, until: until)
            guard values.count <= 25 else { return nil }
            var seen: Set<String> = []
            let posts = values.filter {
              guard let subject = try? AnalyticsSubject($0.uri), subject.did == $0.author.did,
                subject.collection == "app.bsky.feed.post", seen.insert($0.uri).inserted,
                let date = Self.date($0.record.createdAt) else { return false }
              return date >= since && date <= until
            }.map(Self.present)
            return .init(articleUri: article.uri, articleTitle: article.title, articleUrl: article.url, posts: posts)
          } catch { return nil }
        }
      }
      var items: [BlueskyAnalyticsShares] = []
      var available = true
      for await value in group {
        if let value { items.append(value) } else { available = false }
      }
      return (items.sorted { $0.articleUri < $1.articleUri }, available, eligible.count)
    }
  }

  private static func date(_ value: String) -> Date? {
    let format = ISO8601DateFormatter()
    if let date = format.date(from: value) { return date }
    format.formatOptions.insert(.withFractionalSeconds)
    return format.date(from: value)
  }

  private static func present(_ post: BlueskyAnalyticsFeed.Post) -> BlueskyAnalyticsPost {
    let counts = [post.likeCount, post.replyCount, post.repostCount, post.quoteCount]
    let engagement: AnalyticsEngagement? = counts.allSatisfy { ($0 ?? -1) >= 0 }
      ? .init(likes: post.likeCount!, replies: post.replyCount!, reposts: post.repostCount!, quotes: post.quoteCount!) : nil
    let subject = try? AnalyticsSubject(post.uri)
    return .init(uri: post.uri, title: post.record.text,
      url: subject.map { "https://bsky.app/profile/\($0.did)/post/\($0.rkey)" } ?? "",
      publishedAt: post.record.createdAt, engagement: engagement)
  }

  private static func score(_ post: BlueskyAnalyticsPost) -> Int64 {
    guard let value = post.engagement else { return -1 }
    return [value.likes, value.replies, value.reposts, value.quotes].reduce(Int64(0)) { partial, count in
      let (sum, overflow) = partial.addingReportingOverflow(Int64(count))
      return overflow ? Int64.max : sum
    }
  }
}
