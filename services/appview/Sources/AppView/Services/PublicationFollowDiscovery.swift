import AsyncHTTPClient
import GatewayCore
import Foundation
import GatewayCore
import Logging
import ThinAppViewCore

/// Canonical server-side follow-graph publication discovery for thin clients.
enum PublicationFollowDiscovery {
  private static let maxFollows = 500
  private static let followPageLimit = 100
  private static let discoveryConcurrency = 25
  private static let ownPublicationMaxPages = 5
  private static let ownPublicationRetryWindow: Duration = .seconds(3)

  static func discover(
    viewerDid: String,
    auth: AuthContext,
    repo: ATProtoAuthenticatedRepoClient,
    httpClient: HTTPClient,
    plcURL: String,
    logger: Logger
  ) async -> [ProjectionDiscoveredRow] {
    var subjectDids = Set<String>()
    subjectDids.insert(viewerDid)

    // Viewer repo graph.follow (canonical)
    do {
      var cursor: String?
      repeat {
        let page = try await repo.listRecords(
          auth: auth,
          repo: viewerDid,
          collection: PublicationLexicons.graphFollow,
          limit: followPageLimit,
          cursor: cursor,
          reverse: false
        )
        for record in page.records {
          if let subject = record.value.values["subject"] as? String {
            subjectDids.insert(subject)
          }
          if subjectDids.count >= maxFollows { break }
        }
        cursor = page.cursor
      } while cursor != nil && subjectDids.count < maxFollows
    } catch {
      logger.debug("Viewer graph.follow unreadable — merging relay")
    }

    // Bluesky relay getFollows
    if subjectDids.count < maxFollows {
      let relay = await fetchRelayFollows(viewerDid: viewerDid, httpClient: httpClient)
      for did in relay {
        subjectDids.insert(did)
        if subjectDids.count >= maxFollows { break }
      }
    }

    let subjects = Array(subjectDids.prefix(maxFollows))
    var discovered: [ProjectionDiscoveredRow] = []

    // The viewer's own publications come from `discoverOwnPublications`, which runs on every
    // sidebar build rather than only behind follow-graph enumeration.
    let others = subjects.filter { $0 != viewerDid }

    // Sliding window rather than sequential batches. Batching waited for all 25 authors to settle
    // before starting the next 25, so a single unreachable PDS held its whole batch open for the
    // full timeout; across 500 follows that head-of-line blocking dominated the request.
    func discoveryTask(for did: String) -> @Sendable () async -> [ProjectionDiscoveredRow] {
      {
        await discoverAuthor(
          authorDid: did,
          handle: did,
          displayName: nil,
          avatar: nil,
          repo: repo,
          auth: nil,
          httpClient: httpClient,
          plcURL: plcURL
        ) ?? []
      }
    }

    await withTaskGroup(of: [ProjectionDiscoveredRow].self) { group in
      var next = 0
      while next < min(discoveryConcurrency, others.count) {
        group.addTask(operation: discoveryTask(for: others[next]))
        next += 1
      }
      while let rows = await group.next() {
        discovered.append(contentsOf: rows)
        if next < others.count {
          group.addTask(operation: discoveryTask(for: others[next]))
          next += 1
        }
      }
    }

    // Dedupe by publicationId, sort by title
    var byId: [String: ProjectionDiscoveredRow] = [:]
    for row in discovered {
      byId[row.publicationId] = row
    }
    return byId.values.sorted {
      $0.title.localizedCaseInsensitiveCompare($1.title) == .orderedAscending
    }
  }

  /// Lists the viewer's publication records directly from their PDS, querying every publication
  /// collection in parallel. Returns `nil` when the repo could not be read (PLC/PDS failure) so
  /// callers can keep previously known rows instead of treating a transient error as "none".
  static func discoverOwnPublications(
    viewerDid: String,
    auth: AuthContext,
    repo: ATProtoAuthenticatedRepoClient,
    httpClient: HTTPClient,
    plcURL: String,
    logger: Logger
  ) async -> [ProjectionDiscoveredRow]? {
    let collections = PublicationLexicons.discoveryPublicationCollections
    async let pdsBaseTask = try? ATProtoPdsResolution.resolvePdsBase(
      repoDid: viewerDid,
      plcBase: plcURL,
      httpClient: httpClient
    )

    let results = await withTaskGroup(of: (Int, [ATProtoAuthenticatedRepoClient.RepoRecord]?).self) { group in
      for (index, collection) in collections.enumerated() {
        group.addTask {
          let records = await listOwnPublicationRecords(
            viewerDid: viewerDid,
            collection: collection,
            auth: auth,
            repo: repo
          )
          return (index, records)
        }
      }
      var byIndex: [Int: [ATProtoAuthenticatedRepoClient.RepoRecord]?] = [:]
      for await (index, records) in group {
        byIndex[index] = records
      }
      return byIndex
    }

    // Same precedence as follow discovery: the first collection (in lexicon order) with records
    // wins, so a legacy or wrapper collection never shadows `site.standard.publication`.
    let pdsBase = await pdsBaseTask
    let now = Date()
    for index in collections.indices {
      guard let records = results[index] ?? nil, !records.isEmpty else { continue }
      return records.map { record in
        let value = record.value.values
        let title =
          (value["title"] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines).nilIfEmpty
          ?? (value["name"] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines).nilIfEmpty
          ?? "My Publications"
        return ProjectionDiscoveredRow(
          publicationId: record.uri,
          subscriptionPublicationId: record.uri,
          authorDid: viewerDid,
          authorHandle: "You",
          title: title,
          iconUrl: RenderFieldExtractor.publicationIconUrl(from: value, repoDid: viewerDid, pdsBase: pdsBase),
          avatarUrl: nil,
          discoveredAt: now
        )
      }
    }

    if results.values.contains(where: { $0 == nil }) {
      logger.warning(
        "Own publication discovery could not read the viewer repo",
        metadata: ["viewerDid": .string(viewerDid)]
      )
      return nil
    }
    return []
  }

  /// Pages through one publication collection, retrying once after a fast failure (reset, 429,
  /// 5xx). A failure that already ran into the request timeout is not retried, so an outage costs
  /// one timeout rather than two. `nil` means unreadable.
  private static func listOwnPublicationRecords(
    viewerDid: String,
    collection: String,
    auth: AuthContext,
    repo: ATProtoAuthenticatedRepoClient
  ) async -> [ATProtoAuthenticatedRepoClient.RepoRecord]? {
    let clock = ContinuousClock()
    for attempt in 0..<2 {
      if attempt > 0 { try? await Task.sleep(for: .milliseconds(250)) }
      let started = clock.now
      do {
        var records: [ATProtoAuthenticatedRepoClient.RepoRecord] = []
        var cursor: String?
        var pages = 0
        repeat {
          let page = try await repo.listRecords(
            auth: auth,
            repo: viewerDid,
            collection: collection,
            limit: 100,
            cursor: cursor,
            reverse: true,
            requireResolvedRepository: true
          )
          records.append(contentsOf: page.records)
          cursor = page.cursor
          pages += 1
        } while cursor != nil && pages < ownPublicationMaxPages
        return records
      } catch {
        if clock.now - started > ownPublicationRetryWindow { return nil }
      }
    }
    return nil
  }

  private static func discoverAuthor(
    authorDid: String,
    handle: String,
    displayName: String?,
    avatar: String?,
    repo: ATProtoAuthenticatedRepoClient,
    auth: AuthContext?,
    httpClient: HTTPClient,
    plcURL: String
  ) async -> [ProjectionDiscoveredRow]? {
    let now = Date()
    let pdsBase = try? await ATProtoPdsResolution.resolvePdsBase(
      repoDid: authorDid,
      plcBase: plcURL,
      httpClient: httpClient
    )

    for collection in PublicationLexicons.discoveryPublicationCollections {
      let page = try? await repo.listRecords(
        auth: auth,
        repo: authorDid,
        collection: collection,
        limit: 50,
        reverse: true
      )
      guard let records = page?.records, !records.isEmpty else { continue }

      return records.compactMap { record in
        let value = record.value.values
        let title =
          (value["title"] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines).nilIfEmpty
          ?? (value["name"] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines).nilIfEmpty
          ?? displayName
          ?? handle
        let icon =
          RenderFieldExtractor.publicationIconUrl(from: value, repoDid: authorDid, pdsBase: pdsBase)
          ?? avatar.flatMap { RenderFieldExtractor.publicationIconUrl(from: ["avatar": $0], repoDid: authorDid, pdsBase: pdsBase) }
          ?? avatar
        return ProjectionDiscoveredRow(
          publicationId: record.uri,
          subscriptionPublicationId: record.uri,
          authorDid: authorDid,
          authorHandle: handle,
          title: title,
          iconUrl: icon,
          avatarUrl: avatar,
          discoveredAt: now
        )
      }
    }

    for collection in PublicationLexicons.discoveryContentCollections {
      let page = try? await repo.listRecords(
        auth: auth,
        repo: authorDid,
        collection: collection,
        limit: 1,
        reverse: true
      )
      if let records = page?.records, !records.isEmpty {
        return [
          ProjectionDiscoveredRow(
            publicationId: authorDid,
            subscriptionPublicationId: nil,
            authorDid: authorDid,
            authorHandle: handle,
            title: displayName ?? handle,
            iconUrl: avatar,
            avatarUrl: avatar,
            discoveredAt: now
          ),
        ]
      }
    }

    return nil
  }

  static func rowFromPublicationAtUri(
    atUri: String,
    repo: ATProtoAuthenticatedRepoClient,
    auth: AuthContext?,
    httpClient: HTTPClient,
    plcURL: String
  ) async -> ProjectionDiscoveredRow? {
    let normalized = PublicationProjectionLogic.normalizeAtRepoParam(atUri)
    guard let parsed = RenderFieldExtractor.parseAtUri(normalized),
          PublicationLexicons.publicationRecordCollections.contains(parsed.collection)
    else { return nil }

    guard let value = try? await repo.getRecordByAtUri(auth: auth, atUri: normalized) else {
      return nil
    }

    let dict = value.values
    let title =
      (dict["title"] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines).nilIfEmpty
      ?? (dict["name"] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines).nilIfEmpty
      ?? parsed.rkey
    let pdsBase = try? await ATProtoPdsResolution.resolvePdsBase(
      repoDid: parsed.did,
      plcBase: plcURL,
      httpClient: httpClient
    )
    let icon =
      RenderFieldExtractor.publicationIconUrl(from: dict, repoDid: parsed.did, pdsBase: pdsBase)

    return ProjectionDiscoveredRow(
      publicationId: normalized,
      subscriptionPublicationId: normalized,
      authorDid: parsed.did,
      authorHandle: parsed.did,
      title: title,
      iconUrl: icon,
      avatarUrl: nil,
      discoveredAt: Date()
    )
  }

  private static func fetchRelayFollows(viewerDid: String, httpClient: HTTPClient) async -> [String] {
    var all: [String] = []
    var cursor: String?
    repeat {
      var comps = URLComponents(string: "\(ATProtoPdsResolution.bskyAppViewPublic)/xrpc/app.bsky.graph.getFollows")!
      var items = [
        URLQueryItem(name: "actor", value: viewerDid),
        URLQueryItem(name: "limit", value: "100"),
      ]
      if let cursor { items.append(URLQueryItem(name: "cursor", value: cursor)) }
      comps.queryItems = items
      guard let url = comps.url?.absoluteString else { break }

      var request = HTTPClientRequest(url: url)
      request.headers.add(name: "Accept", value: "application/json")
      guard
        let response = try? await httpClient.execute(request, timeout: .seconds(15)),
        response.status == .ok,
        let body = try? await response.body.collect(upTo: 256 * 1024),
        let json = try? JSONSerialization.jsonObject(with: Data(buffer: body)) as? [String: Any]
      else { break }

      let follows = (json["follows"] as? [[String: Any]]) ?? []
      all.append(contentsOf: follows.compactMap { $0["did"] as? String })
      cursor = json["cursor"] as? String
    } while cursor != nil && all.count < maxFollows
    return all
  }
}

private extension String {
  var nilIfEmpty: String? {
    isEmpty ? nil : self
  }
}
