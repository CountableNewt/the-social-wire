enum AnalyticsError: Error, Equatable {
  case invalid, unavailable, rateLimit, providerUnavailable
}
