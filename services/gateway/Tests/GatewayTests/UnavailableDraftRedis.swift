import Foundation
import SocialWireRedis

actor UnavailableDraftRedis: RedisCommandClient {
  enum Failure: Error { case unavailable }
  func get(_ key: String) throws -> Data? { throw Failure.unavailable }
  func set(_ key: String, value: Data, expirationMilliseconds: Int) throws { throw Failure.unavailable }
  func setIfAbsent(_ key: String, value: Data, expirationMilliseconds: Int) throws -> Bool { throw Failure.unavailable }
  func delete(_ keys: [String]) throws -> Int { throw Failure.unavailable }
  func execute(command: String, arguments: [RedisCommandValue]) throws -> RedisCommandValue { throw Failure.unavailable }
  func ping() throws { throw Failure.unavailable }
  func shutdown() {}
}
