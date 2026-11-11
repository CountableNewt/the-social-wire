import Foundation

struct PublicationAnalyticsProvider: Sendable {
  let reader: any AnalyticsPublicReading
  let count: @Sendable (String, String, String) async throws -> Int

  private static let publicationsCollection = "site.standard.publication"
  private static let documentsCollection = "site.standard.document"
  private static let publicationLimit = 20
  private static let recommendationLimit = 40

  func summary(author: String, at: Date) async throws -> PublicationAnalyticsSummary {
    async let publicationInventory = inventory(author: author, collection: Self.publicationsCollection, pages: 1)
    async let documentInventory = try? inventory(author: author, collection: Self.documentsCollection, pages: 3)
    let (records, publicationsComplete) = try await publicationInventory
    let publications = Array(records.prefix(Self.publicationLimit))
    if publications.isEmpty {
      return .init(sampledAt: at, publications: [], inventoryComplete: publicationsComplete)
    }
    async let subscriberCounts = counts(
      targets: publications.map(\.uri), collection: "site.standard.graph.subscription", path: ".publication"
    )
    let (documents, documentsComplete) = await documentInventory ?? ([], false)
    let publicationURIs = Set(publications.map(\.uri))
    var documentsByPublication: [String: [String]] = [:]
    var recommendationTargets: [String] = []
    for document in documents {
      guard let site = document.value.values["site"] as? String, publicationURIs.contains(site) else { continue }
      documentsByPublication[site, default: []].append(document.uri)
      if recommendationTargets.count < Self.recommendationLimit { recommendationTargets.append(document.uri) }
    }
    let recommendations = try await counts(
      targets: recommendationTargets, collection: "site.standard.graph.recommend", path: ".document"
    )
    let subscribers = try await subscriberCounts
    let rows = publications.map { publication in
      let uris = documentsByPublication[publication.uri] ?? []
      let observed = uris.compactMap { recommendations[$0] }
      let complete = documentsComplete && observed.count == uris.count
      let total = observed.reduce(0) { sum, value in
        let (result, overflow) = sum.addingReportingOverflow(value)
        return overflow ? Int.max : result
      }
      return PublicationAnalyticsRow(
        uri: publication.uri,
        name: publication.value.values["name"] as? String ?? "Untitled Publication",
        url: publication.value.values["url"] as? String ?? "",
        documents: uris.count,
        subscribers: subscribers[publication.uri],
        recommendations: complete || !observed.isEmpty ? total : nil,
        recommendationCoverageComplete: complete
      )
    }
    return .init(sampledAt: at, publications: rows,
      inventoryComplete: documentsComplete && publicationsComplete && records.count <= Self.publicationLimit)
  }

  private func inventory(author: String, collection: String, pages: Int) async throws -> ([AnalyticsPublicRecord], Bool) {
    var records: [AnalyticsPublicRecord] = []
    var seen: Set<String> = []
    var cursors: Set<String> = []
    var cursor: String?
    for _ in 0..<pages {
      try Task.checkCancellation()
      let page = try await reader.records(author: author, collection: collection, cursor: cursor)
      guard page.records.count <= 100 else { throw AnalyticsError.providerUnavailable }
      for record in page.records {
        guard let subject = try? AnalyticsSubject(record.uri, publication: collection == Self.publicationsCollection),
          subject.did == author, subject.collection == collection,
          record.value.values["$type"] as? String == collection else { throw AnalyticsError.providerUnavailable }
        if seen.insert(record.uri).inserted { records.append(record) }
      }
      guard let next = page.cursor, !next.isEmpty else { return (records, true) }
      guard cursors.insert(next).inserted else { return (records, false) }
      cursor = next
    }
    return (records, false)
  }

  private func counts(targets: [String], collection: String, path: String) async throws -> [String: Int] {
    var counts: [String: Int] = [:]
    let concurrency = 8
    for start in stride(from: 0, to: targets.count, by: concurrency) {
      try Task.checkCancellation()
      await withTaskGroup(of: (String, Int?).self) { group in
        for uri in targets[start..<min(start + concurrency, targets.count)] {
          group.addTask { (uri, try? await count(uri, collection, path)) }
        }
        for await (uri, value) in group { counts[uri] = value }
      }
    }
    try Task.checkCancellation()
    return counts
  }
}
