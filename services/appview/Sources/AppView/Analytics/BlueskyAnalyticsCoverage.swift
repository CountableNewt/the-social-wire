struct BlueskyAnalyticsCoverage: Codable, Sendable {
  var feedComplete = false
  var feedAvailable = false
  var sharesAvailable = true
  var sharesComplete = false
  var articlesSearched = 0
  var feedPostsExamined = 0
}
