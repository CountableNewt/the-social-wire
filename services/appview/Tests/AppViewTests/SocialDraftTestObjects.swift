import Foundation
@testable import AppView

actor SocialDraftTestObjects: SocialDraftObjectStoring {
  private var bytes: [String: Data] = [:]
  private var failDelete = false
  func setDeleteFailure(_ value: Bool) { failDelete = value }
  func put(key: String, file: SocialDraftTemporaryFile, length: Int) throws {
    let value = try Data(contentsOf: file.url)
    guard value.count == length else { throw SocialDraftStorageError.invalid }
    bytes[key] = value
  }
  func get(key: String, into file: SocialDraftTemporaryFile, maximumBytes: Int) throws {
    guard let value = bytes[key], value.count <= maximumBytes else { throw SocialDraftStorageError.notFound }
    try value.write(to: file.url)
  }
  func delete(key: String) throws {
    if failDelete { throw SocialDraftStorageError.unavailable }
    bytes.removeValue(forKey: key)
  }
}
