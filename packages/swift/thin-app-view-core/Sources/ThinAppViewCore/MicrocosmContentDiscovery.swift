import AsyncHTTPClient
import Foundation
import NIOCore

/// Bounded public backlink discovery and record hydration. No bearer/DPoP tokens
/// are sent to providers. Provider omission proves neither complete history nor deletion.
public struct MicrocosmContentDiscovery: PublicContentDiscovering {
  private let indexer: ThinAppViewIndexer
  private let transport: any PDSHTTPTransport
  private let constellation: String
  private let slingshot: String
  private let timeout: Duration
  static let pageSize = 25
  static let maximumPages = 2
  static let maximumRecords = 50

  public init(indexer: ThinAppViewIndexer, httpClient: HTTPClient) {
    self.init(indexer: indexer, transport: MicrocosmPublicHTTPTransport(httpClient: httpClient))
  }

  init(indexer: ThinAppViewIndexer, transport: any PDSHTTPTransport,
    constellation: String = "https://constellation.microcosm.blue",
    slingshot: String = "https://slingshot.microcosm.blue", timeout: Duration = .seconds(12)) {
    self.indexer = indexer
    self.transport = transport
    self.constellation = constellation
    self.slingshot = slingshot
    self.timeout = timeout
  }

  public func discover(scope: PublicContentDiscoveryScope) async throws -> Int {
    try Task.checkCancellation()
    return try await withThrowingTaskGroup(of: Int.self) { group in
      defer { group.cancelAll() }
      group.addTask { try await self.load(scope: scope) }
      group.addTask {
        try await Task.sleep(for: timeout)
        throw PublicContentDiscoveryError.unavailable
      }
      guard let result = try await group.next() else { throw CancellationError() }
      return result
    }
  }

  private func load(scope: PublicContentDiscoveryScope) async throws -> Int {
    var observed: Set<String> = []
    var indexed = 0
    var requests = 0
    for target in scope.publicationKeys {
      for collection in ["site.standard.document", "site.standard.entry"] {
        var cursor: String?
        var cursors: Set<String> = []
        for _ in 0..<Self.maximumPages {
          // The whole scope has a finite request/record budget, including empty pages.
          guard requests < 8, observed.count < Self.maximumRecords else { return indexed }
          requests += 1
          var query = [URLQueryItem(name: "subject", value: target),
            .init(name: "source", value: collection + ":site"),
            .init(name: "did", value: scope.authorDid),
            // Constellation uses false for newest first; true would spend the cap on oldest records.
            .init(name: "limit", value: String(Self.pageSize)), .init(name: "reverse", value: "false")]
          if let cursor { query.append(.init(name: "cursor", value: cursor)) }
          let response = try await json(base: constellation, path: "/xrpc/blue.microcosm.links.getBacklinks", query: query)
          guard let records = response["records"] as? [[String: Any]], records.count <= Self.pageSize,
            response["cursor"] == nil || response["cursor"] is NSNull || response["cursor"] is String
          else { throw PublicContentDiscoveryError.invalidResponse }
          for link in records {
            try Task.checkCancellation()
            guard observed.count < Self.maximumRecords else { return indexed }
            guard let did = link["did"] as? String, did == scope.authorDid,
              link["collection"] as? String == collection, let rkey = link["rkey"] as? String,
              !rkey.isEmpty, rkey.utf8.count <= 512,
              !rkey.contains(where: { "/?#%\\ \r\n\t".contains($0) })
            else { throw PublicContentDiscoveryError.invalidResponse }
            let uri = "at://\(did)/\(collection)/\(rkey)"
            guard observed.insert(uri).inserted else { continue }
            // This is only a download optimization. The store's atomic insert
            // still protects a canonical row created after this lookup.
            if try await indexer.hasKnownContent(uri: uri) { continue }
            let record: [String: Any]
            do {
              record = try await json(base: slingshot, path: "/xrpc/com.atproto.repo.getRecord",
                query: [.init(name: "repo", value: did), .init(name: "collection", value: collection), .init(name: "rkey", value: rkey)])
            } catch PublicContentDiscoveryError.unavailable { continue }
            guard record["uri"] as? String == uri, let cid = record["cid"] as? String,
              let parsedCID = try? RepositoryCID(cid), parsedCID.isDAGCBOR,
              let value = record["value"] as? [String: Any], value["$type"] as? String == collection,
              scope.contains(site: RenderFieldExtractor.publicationSiteField(from: value))
            else { throw PublicContentDiscoveryError.invalidResponse }
            let json = try JSONSerialization.data(withJSONObject: value)
            do { try PublicContentRecordCID.verify(json: json, cid: cid) }
            catch { throw PublicContentDiscoveryError.invalidResponse }
            try Task.checkCancellation()
            let outcome = try await indexer.handleCommitWithOutcome(repoDid: did, collection: collection, rkey: rkey,
              cid: cid, recordJSON: json, operation: "create",
              ingestionSource: "microcosm_on_demand")
            if outcome.didMutateProjection { indexed += 1 }
          }
          guard let next = response["cursor"] as? String, !next.isEmpty else { break }
          guard next.utf8.count <= 2048, cursors.insert(next).inserted else { throw PublicContentDiscoveryError.invalidResponse }
          cursor = next
        }
      }
    }
    return indexed
  }

  private func json(base: String, path: String, query: [URLQueryItem]) async throws -> [String: Any] {
    try Task.checkCancellation()
    guard var components = URLComponents(string: base), components.scheme == "https",
      components.host?.isEmpty == false, components.user == nil, components.password == nil,
      components.query == nil, components.fragment == nil else { throw PublicContentDiscoveryError.invalidScope }
    components.path = path
    components.queryItems = query
    guard let url = components.url else { throw PublicContentDiscoveryError.invalidScope }
    var request = HTTPClientRequest(url: url.absoluteString)
    request.headers.add(name: "Accept", value: "application/json")
    request.headers.add(name: "User-Agent", value: "TheSocialWire/1 (+https://thesocialwire.app)")
    let response = try await transport.execute(request, timeout: .seconds(8))
    guard response.status == .ok else {
      try await HTTPResponseBodyDrain.drainOrCancel(response.body)
      throw PublicContentDiscoveryError.unavailable
    }
    let body = try await response.body.collect(upTo: 2 << 20)
    try Task.checkCancellation()
    guard let value = try JSONSerialization.jsonObject(with: Data(buffer: body)) as? [String: Any] else { throw PublicContentDiscoveryError.invalidResponse }
    return value
  }
}
