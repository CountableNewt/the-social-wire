struct AppViewEnrollmentPartialFailure: Error, Sendable, CustomStringConvertible {
  let indexed: Int
  let failures: [AppViewRSSRefreshFailure]

  var description: String { "Some RSS sources could not be refreshed" }
}
