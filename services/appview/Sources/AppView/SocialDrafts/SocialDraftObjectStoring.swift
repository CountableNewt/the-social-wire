import Foundation

protocol SocialDraftObjectStoring: Sendable {
  func put(key: String, file: SocialDraftTemporaryFile, length: Int) async throws
  func get(key: String, into file: SocialDraftTemporaryFile, maximumBytes: Int) async throws
  func delete(key: String) async throws
}
