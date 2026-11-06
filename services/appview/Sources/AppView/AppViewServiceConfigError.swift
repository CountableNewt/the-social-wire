enum AppViewServiceConfigError: Error, Equatable {
  case invalidPublicContentDiscoveryMode
  case invalidEnvironment
  case missingDatabaseURL
  case invalidPort
  case invalidServiceURL
}
