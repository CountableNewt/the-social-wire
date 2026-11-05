import Foundation

/// Only successful canonical public reads are cached; both cache and in-flight work are bounded.
actor AnalyticsValidatedContent: AnalyticsContentProviding {
  let base: any AnalyticsContentProviding
  private var cache: [String: Date] = [:]
  private var inflight: [String: Task<Void, Error>] = [:]
  init(base: any AnalyticsContentProviding) { self.base = base }

  func inventory(author: String, at: Date) async throws -> (items: [AnalyticsItem], complete: Bool) {
    try await base.inventory(author: author, at: at)
  }

  func validate(_ uri: String) async throws {
    try Task.checkCancellation()
    if let until = cache[uri], Date() < until { return }
    if let existing = inflight[uri] { try await existing.value; try Task.checkCancellation(); return }
    guard inflight.count < 1000 else { throw AnalyticsError.unavailable }
    let task = Task { try await base.validate(uri) }
    inflight[uri] = task
    defer { inflight.removeValue(forKey: uri) }
    try await withTaskCancellationHandler { try await task.value } onCancel: { task.cancel() }
    try Task.checkCancellation()
    cache = cache.filter { Date() < $0.value }
    if cache.count < 10000 { cache[uri] = Date().addingTimeInterval(300) }
  }
}
