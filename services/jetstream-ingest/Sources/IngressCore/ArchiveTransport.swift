import AsyncHTTPClient
import CZstd
import Foundation
import NIOCore

public actor ArchiveTransport {
  let config: IngressConfiguration
  let client: HTTPClient
  let origin: URL
  let budget: ReplayBudget
  let recordUsage: @Sendable (Int64) async throws -> Void
  public private(set) var retryCount: Int
  public private(set) var rangeResumeCount: Int
  public private(set) var etag: String?

  public init(
    config: IngressConfiguration, client: HTTPClient, budget: ReplayBudget,
    checkpoint: IngressCheckpoint?, recordUsage: @escaping @Sendable (Int64) async throws -> Void
  ) {
    self.config = config
    self.client = client
    self.budget = budget
    self.recordUsage = recordUsage
    self.origin = URL(string: "https://" + config.host)!
    retryCount = checkpoint?.retries ?? 0
    rangeResumeCount = checkpoint?.resumes ?? 0
    etag = checkpoint?.etag
  }
  init(
    config: IngressConfiguration, client: HTTPClient, budget: ReplayBudget, origin: URL,
    recordUsage: @escaping @Sendable (Int64) async throws -> Void
  ) {
    self.config = config
    self.client = client
    self.budget = budget
    self.recordUsage = recordUsage
    self.origin = origin
    retryCount = 0
    rangeResumeCount = 0
    etag = nil
  }
  public func liveDictionary() async throws -> Data {
    let endpoint = origin.appendingPathComponent("xrpc/network.bsky.jetstream.getZstdDictionary")
    var request = HTTPClientRequest(url: endpoint.absoluteString)
    request.headers.add(name: "Cache-Control", value: "no-cache")
    let response = try await client.execute(request, timeout: .seconds(15))
    guard response.status == .ok else {
      throw IngressError.provider("Live compression dictionary unavailable")
    }
    let buffer = try await response.body.collect(upTo: 8 << 20)
    let data = Data(buffer.readableBytesView)
    guard data.withUnsafeBytes({ ZSTD_getDictID_fromDict($0.baseAddress, $0.count) }) != 0 else {
      throw IngressError.provider("Invalid live compression dictionary")
    }
    return data
  }
  public func plan(after: Int64, before: Int64?) async throws -> SnapshotPlan {
    let payload: JSONValue = .object(
      [
        "collections": .array(config.collections.map(JSONValue.string)),
        "afterSeq": .integer(after),
      ].merging(before.map { ["beforeSeq": .integer($0)] } ?? [:]) { first, _ in first })
    let file = try await download(
      path: "/xrpc/network.bsky.jetstream.planSnapshot", parameters: [],
      body: JSONEncoder().encode(payload), expectedETag: nil, maximumBytes: 16 << 20)
    defer { try? FileManager.default.removeItem(at: file) }
    return try JSONDecoder().decode(SnapshotPlan.self, from: Data(contentsOf: file))
  }
  public func segment(_ segment: SnapshotPlan.Segment) async throws -> URL {
    let path = "/xrpc/network.bsky.jetstream.getSegment"
    let parameters = [URLQueryItem(name: "segment", value: segment.name)]
    let expectedETag = "\"\(segment.checksum)\""
    if config.segmentStripes > 1, let size = try await probeSegment(segment), size > 1 {
      let count = min(config.segmentStripes, size)
      let stride = (size + count - 1) / count
      let parts = try await withThrowingTaskGroup(of: (Int, ArchiveSpool).self) { group in
        for index in 0..<count {
          let start = index * stride
          guard start < size else { break }
          let end = min(size - 1, start + stride - 1)
          group.addTask { [self] in
            let url = try await download(
              path: path, parameters: parameters, body: nil, expectedETag: expectedETag,
              maximumBytes: end - start + 1, rangeStart: start, rangeEnd: end)
            return (index, ArchiveSpool(url))
          }
        }
        defer { group.cancelAll() }
        var output: [(Int, ArchiveSpool)] = []
        for try await part in group { output.append(part) }
        return output.sorted { $0.0 < $1.0 }
      }
      let merged = FileManager.default.temporaryDirectory.appendingPathComponent(
        "socialwire-ingress-" + UUID().uuidString)
      guard
        FileManager.default.createFile(
          atPath: merged.path, contents: nil, attributes: [.posixPermissions: 0o600])
      else { throw IngressError.provider("Cannot create merged archive spool") }
      let output = try FileHandle(forWritingTo: merged)
      do {
        for (_, part) in parts {
          let input = try FileHandle(forReadingFrom: part.url)
          defer { try? input.close() }
          while let chunk = try input.read(upToCount: 1 << 20), !chunk.isEmpty {
            try Task.checkCancellation()
            try output.write(contentsOf: chunk)
          }
        }
        try output.synchronize()
        try output.close()
        return merged
      } catch {
        try? output.close()
        try? FileManager.default.removeItem(at: merged)
        throw error
      }
    }
    return try await download(
      path: path, parameters: parameters, body: nil, expectedETag: expectedETag,
      maximumBytes: 4 << 30)
  }
  private func probeSegment(_ segment: SnapshotPlan.Segment) async throws -> Int? {
    try await budget.check()
    var url = URLComponents(url: origin, resolvingAgainstBaseURL: false)!
    url.path = "/xrpc/network.bsky.jetstream.getSegment"
    url.queryItems = [URLQueryItem(name: "segment", value: segment.name)]
    guard let endpoint = url.url else {
      throw IngressError.invalidConfiguration("Invalid segment endpoint")
    }
    var request = HTTPClientRequest(url: endpoint.absoluteString)
    request.headers.add(name: "Authorization", value: "Bearer " + config.apiKey)
    request.headers.add(name: "Range", value: "bytes=0-0")
    let response = try await client.execute(request, timeout: .seconds(30))
    guard response.status == .partialContent else { return nil }
    guard response.headers.first(name: "ETag") == "\"\(segment.checksum)\"",
      let raw = response.headers.first(name: "Content-Range"), let range = Self.contentRange(raw),
      range.start == 0, range.end == 0, range.total <= 4 << 30
    else { throw IngressError.malformedArchive("Invalid segment range probe") }
    let data = try await response.body.collect(upTo: 1)
    guard data.readableBytes == 1 else {
      throw IngressError.malformedArchive("Truncated segment probe")
    }
    try await recordUsage(1)
    try await budget.add(1)
    return range.total
  }
  public func block(_ segment: SnapshotPlan.Segment, index: Int) async throws -> Data {
    let file = try await download(
      path: "/xrpc/network.bsky.jetstream.getBlock",
      parameters: [
        .init(name: "segment", value: segment.name),
        .init(name: "blockIndex", value: String(index)),
      ], body: nil, expectedETag: "\"\(segment.checksum):\(index)\"",
      maximumBytes: ArchiveBlockDecoder.maxBlockBytes)
    defer { try? FileManager.default.removeItem(at: file) }
    return try Data(contentsOf: file)
  }
  public func progress(state: String, after: Int64, sealed: Int64) async -> ReplayProgress {
    ReplayProgress(
      state: state, after: after, sealed: sealed, bytes: await budget.incident, retries: retryCount,
      resumes: rangeResumeCount, etag: etag)
  }
  private func download(
    path: String, parameters: [URLQueryItem], body: Data?, expectedETag: String?, maximumBytes: Int,
    rangeStart: Int = 0, rangeEnd: Int? = nil
  ) async throws -> URL {
    var url = URLComponents(url: origin, resolvingAgainstBaseURL: false)!
    url.path = path
    url.queryItems = parameters.isEmpty ? nil : parameters
    guard let endpoint = url.url else {
      throw IngressError.invalidConfiguration("Invalid archive endpoint")
    }
    let fileURL = FileManager.default.temporaryDirectory.appendingPathComponent(
      "socialwire-ingress-" + UUID().uuidString)
    guard
      FileManager.default.createFile(
        atPath: fileURL.path, contents: nil, attributes: [.posixPermissions: 0o600])
    else { throw IngressError.provider("Cannot create archive spool") }
    let output = try FileHandle(forWritingTo: fileURL)
    var keep = false
    defer {
      try? output.close()
      if !keep { try? FileManager.default.removeItem(at: fileURL) }
    }
    var offset = 0
    var total: Int?
    var pinnedETag: String?
    for attempt in 0..<config.maxDownloadAttempts {
      try Task.checkCancellation()
      try await budget.check()
      if attempt > 0 { retryCount += 1 }
      var request = HTTPClientRequest(url: endpoint.absoluteString)
      request.method = body == nil ? .GET : .POST
      request.headers.add(name: "Authorization", value: "Bearer " + config.apiKey)
      if let body {
        request.headers.add(name: "Content-Type", value: "application/json")
        request.body = .bytes(body)
      }
      if body == nil {
        request.headers.add(
          name: "Range", value: "bytes=\(rangeStart + offset)-\(rangeEnd.map(String.init) ?? "")")
        if let pinnedETag { request.headers.add(name: "If-Range", value: pinnedETag) }
        if offset > 0 { rangeResumeCount += 1 }
      }
      var retryAfter: Double = min(config.backoffMax, config.backoffMin * pow(2, Double(attempt)))
      do {
        let response = try await client.execute(request, timeout: .seconds(300))
        if response.status.code == 429 || response.status.code >= 500 {
          retryAfter = Self.retryDelay(
            response.headers.first(name: "Retry-After"), fallback: retryAfter)
          throw IngressError.provider("Archive provider temporarily unavailable")
        }
        guard response.status == .ok || response.status == .partialContent else {
          if response.status.code == 400 {
            let bytes = try await response.body.collect(upTo: 4096)
            let error = try? JSONDecoder().decode(
              JSONValue.self, from: Data(bytes.readableBytesView))
            if error?["error"]?.string == "FutureCursor" { throw IngressError.sourceDiscontinuity }
          }
          throw IngressError.provider("Archive request rejected (\(response.status.code))")
        }
        let validator = response.headers.first(name: "ETag")
        if let expectedETag, validator != expectedETag {
          throw IngressError.malformedArchive("Archive ETag does not match planned generation")
        }
        if let pinnedETag, validator != pinnedETag {
          throw IngressError.malformedArchive("Archive generation changed during resume")
        }
        if response.status == .partialContent {
          guard let raw = response.headers.first(name: "Content-Range"),
            let range = Self.contentRange(raw), range.start == rangeStart + offset,
            range.end <= (rangeEnd ?? range.total - 1), range.total >= rangeStart,
            rangeEnd.map { $0 < range.total } ?? (range.total <= maximumBytes)
          else { throw IngressError.malformedArchive("Invalid archive Content-Range") }
          let expectedLength = rangeEnd.map { $0 - rangeStart + 1 } ?? range.total
          if let total, total != expectedLength {
            throw IngressError.malformedArchive("Archive length changed during resume")
          }
          total = expectedLength
        } else {
          guard rangeStart == 0 && rangeEnd == nil else {
            throw IngressError.malformedArchive("Provider stopped honoring segment stripe ranges")
          }
          if offset > 0 {
            try output.truncate(atOffset: 0)
            try output.seek(toOffset: 0)
            offset = 0
          }
          total = response.headers.first(name: "Content-Length").flatMap(Int.init)
          if let total, total > maximumBytes {
            throw IngressError.malformedArchive("Archive response exceeds spool limit")
          }
        }
        pinnedETag = validator
        etag = validator
        for try await buffer in response.body {
          try Task.checkCancellation()
          let data = Data(buffer.readableBytesView)
          // Persist observed bytes before applying limits; failed requests still consume budget.
          try await recordUsage(Int64(data.count))
          try await budget.add(Int64(data.count))
          guard data.count <= maximumBytes - offset else {
            throw IngressError.malformedArchive("Archive response exceeds spool limit")
          }
          try output.write(contentsOf: data)
          offset += data.count
        }
        if let total, offset != total { throw IngressError.provider("Short archive response") }
        try output.synchronize()
        keep = true
        return fileURL
      } catch {
        if Task.isCancelled { throw CancellationError() }
        if case IngressError.sourceDiscontinuity = error { throw error }
        if case IngressError.incidentBudget = error { throw error }
        if case IngressError.dailyBudget = error { throw error }
        if case IngressError.malformedArchive = error { throw error }
        if attempt == config.maxDownloadAttempts - 1 { throw error }
        if body != nil || pinnedETag == nil {
          try output.truncate(atOffset: 0)
          try output.seek(toOffset: 0)
          offset = 0
          total = nil
        }
        try await Task.sleep(for: .milliseconds(Int64(retryAfter * 1000)))
      }
    }
    throw IngressError.provider("Archive download attempts exhausted")
  }
  public static func contentRange(_ value: String) -> (start: Int, end: Int, total: Int)? {
    guard value.hasPrefix("bytes ") else { return nil }
    let components = value.dropFirst(6).split(separator: "/")
    guard components.count == 2, let total = Int(components[1]) else { return nil }
    let ends = components[0].split(separator: "-")
    guard ends.count == 2, let start = Int(ends[0]), let end = Int(ends[1]), start >= 0,
      end >= start, end < total
    else { return nil }
    return (start, end, total)
  }
  public static func retryDelay(_ value: String?, fallback: Double, now: Date = Date()) -> Double {
    guard let value else { return fallback }
    if let seconds = Double(value), seconds.isFinite, seconds >= 0 { return min(seconds, 86400) }
    let formatter = DateFormatter()
    formatter.locale = Locale(identifier: "en_US_POSIX")
    formatter.timeZone = TimeZone(secondsFromGMT: 0)
    formatter.dateFormat = "EEE, dd MMM yyyy HH:mm:ss zzz"
    return formatter.date(from: value).map { min(86400, max(0, $0.timeIntervalSince(now))) }
      ?? fallback
  }
}
