import Testing

@testable import IndexingWorkerCore

@Suite("Explicit reconciliation retry scope")
struct ReconciliationRetryScopeTests {
  @Test func acceptsOnlyAnExplicitBoundedOperatorInvocation() throws {
    let arguments = [
      "--environment", "prod", "--generation", "fixture-generation", "--expected-count", "4",
      "--apply",
    ]
    let scope = try ReconciliationRetryScope(arguments: arguments)
    let expected = try ReconciliationRetryScope(
      environment: "prod", generation: "fixture-generation", expectedCount: 4)
    #expect(scope == expected)
    #expect(
      try ReconciliationRetryScope(arguments: [
        "--environment=dev", "--generation=v2:us-west.1", "--expected-count=100", "--apply=true",
      ]).expectedCount == 100)
    for invalid in [
      [], ["-"], Array(arguments.dropLast()), arguments + ["extra"],
      ["--environment", "local", "--generation", "generation", "--expected-count", "4", "--apply"],
      ["--environment", "prod", "--expected-count", "4", "--apply"],
      ["--environment", "prod", "--generation", "wildcard*", "--expected-count", "4", "--apply"],
      ["--environment", "prod", "--generation", "generation", "--expected-count", "0", "--apply"],
      [
        "--environment", "prod", "--generation", "generation", "--expected-count", "101", "--apply",
      ],
      arguments + ["--apply=false"], arguments + ["--unknown"], arguments + ["--generation"],
    ] {
      #expect(throws: ReconciliationRetryError.invalidScope) {
        try ReconciliationRetryScope(arguments: invalid)
      }
    }
  }
}
