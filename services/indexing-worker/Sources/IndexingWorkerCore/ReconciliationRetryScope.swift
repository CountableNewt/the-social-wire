public struct ReconciliationRetryScope: Sendable, Equatable {
  public let environment: String
  public let generation: String
  public let expectedCount: Int

  public init(environment: String, generation: String, expectedCount: Int) throws {
    let allowed = Array("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._:-".utf8)
    let alphanumeric = Array("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789".utf8)
    guard ["dev", "prod"].contains(environment), (1...100).contains(expectedCount),
      (1...256).contains(generation.utf8.count),
      generation.utf8.first.map(alphanumeric.contains) == true,
      generation.utf8.allSatisfy(allowed.contains)
    else { throw ReconciliationRetryError.invalidScope }
    self.environment = environment
    self.generation = generation
    self.expectedCount = expectedCount
  }

  public init(arguments: [String]) throws {
    var values: [String: String] = [:]
    var apply = false
    var position = 0
    while position < arguments.count {
      let argument = arguments[position]
      guard argument.hasPrefix("-") else { throw ReconciliationRetryError.invalidScope }
      let parts = argument.dropFirst(argument.hasPrefix("--") ? 2 : 1).split(
        separator: "=", maxSplits: 1,
        omittingEmptySubsequences: false)
      guard let first = parts.first, !first.isEmpty else {
        throw ReconciliationRetryError.invalidScope
      }
      let name = String(first)
      if name == "apply" {
        if parts.count == 1 {
          apply = true
        } else if parts[1] == "true" {
          apply = true
        } else if parts[1] == "false" {
          apply = false
        } else {
          throw ReconciliationRetryError.invalidScope
        }
      } else {
        guard ["environment", "generation", "expected-count"].contains(name) else {
          throw ReconciliationRetryError.invalidScope
        }
        if parts.count == 2 {
          values[name] = String(parts[1])
        } else {
          position += 1
          guard position < arguments.count else { throw ReconciliationRetryError.invalidScope }
          values[name] = arguments[position]
        }
      }
      position += 1
    }
    guard apply, let environment = values["environment"], let generation = values["generation"],
      let expected = values["expected-count"].flatMap(Int.init)
    else { throw ReconciliationRetryError.invalidScope }
    try self.init(environment: environment, generation: generation, expectedCount: expected)
  }
}
