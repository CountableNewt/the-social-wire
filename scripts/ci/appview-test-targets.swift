// Appended only inside the CI test image. Production uses the original manifest.
package.targets += [
  .testTarget(
    name: "ReadStateCoreTests",
    dependencies: [.product(name: "ReadStateCore", package: "thin-app-view-core")],
    path: ".ci-tests/thin-app-view-core/Dependencies/ReadStateCore/Tests/ReadStateCoreTests",
    swiftSettings: [.swiftLanguageMode(.v6)]
  ),
  .testTarget(
    name: "ThinAppViewCoreTests",
    dependencies: [
      .product(name: "ThinAppViewCore", package: "thin-app-view-core"),
      .product(name: "OperationsCore", package: "operations-core"),
      .product(name: "SocialWireRedis", package: "social-wire-redis"),
      .product(name: "GRDB", package: "GRDB.swift"),
      .product(name: "Logging", package: "swift-log"),
    ],
    path: ".ci-tests/thin-app-view-core/Tests/ThinAppViewCoreTests",
    resources: [.copy("Fixtures")],
    swiftSettings: [.swiftLanguageMode(.v6)]
  ),
]
