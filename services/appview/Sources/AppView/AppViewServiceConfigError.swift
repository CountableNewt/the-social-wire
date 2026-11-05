enum AppViewServiceConfigError: Error, Equatable {
  case invalidEnvironment
  case missingDatabaseURL
  case invalidPort
  case invalidServiceURL
}
