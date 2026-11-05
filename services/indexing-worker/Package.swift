// swift-tools-version: 6.0
import PackageDescription

let package = Package(
  name: "SocialWireIndexingWorker",
  platforms: [.macOS(.v14)],
  products: [
    .library(name: "IndexingWorkerCore", targets: ["IndexingWorkerCore"]),
    .executable(name: "IndexingWorker", targets: ["IndexingWorker"]),
    .executable(name: "retry-reconciliation", targets: ["RetryReconciliation"]),
  ],
  dependencies: [
    .package(
      url: "https://github.com/Stygian-Tech/thin-app-view-core.git",
      revision: "dc0e90186485455db78ad0168d861d0dcc8fcae3"),
    .package(
      url: "https://github.com/Stygian-Tech/operations-core.git",
      revision: "a3b7bb328189a423f3e2d2fdd1dcfd4022faddc1"),
    .package(url: "https://github.com/swift-server/async-http-client.git", from: "1.23.0"),
    .package(url: "https://github.com/apple/swift-argument-parser.git", from: "1.4.0"),
    .package(url: "https://github.com/apple/swift-log.git", from: "1.6.0"),
    .package(url: "https://github.com/apple/swift-nio.git", from: "2.65.0"),
    .package(url: "https://github.com/apple/swift-nio-ssl.git", from: "2.25.0"),
    .package(url: "https://github.com/vapor/postgres-nio.git", from: "1.21.0"),
  ],
  targets: [
    .target(name: "IndexingWorkerCore", swiftSettings: [.swiftLanguageMode(.v6)]),
    .target(
      name: "IndexingWorkerHost",
      dependencies: [
        "IndexingWorkerCore",
        .product(name: "ThinAppViewCore", package: "thin-app-view-core"),
        .product(name: "OperationsCore", package: "operations-core"),
        .product(name: "AsyncHTTPClient", package: "async-http-client"),
        .product(name: "Logging", package: "swift-log"),
        .product(name: "NIOCore", package: "swift-nio"),
        .product(name: "NIOHTTP1", package: "swift-nio"),
        .product(name: "NIOPosix", package: "swift-nio"),
        .product(name: "NIOSSL", package: "swift-nio-ssl"),
        .product(name: "PostgresNIO", package: "postgres-nio"),
      ], path: "Sources/AppViewWorkerCore", swiftSettings: [.swiftLanguageMode(.v6)]),
    .executableTarget(
      name: "IndexingWorker",
      dependencies: [
        "IndexingWorkerCore", "IndexingWorkerHost",
        .product(name: "ArgumentParser", package: "swift-argument-parser"),
        .product(name: "Logging", package: "swift-log"),
      ], swiftSettings: [.swiftLanguageMode(.v6)]),
    .executableTarget(
      name: "RetryReconciliation",
      dependencies: ["IndexingWorkerCore", "IndexingWorkerHost"],
      swiftSettings: [.swiftLanguageMode(.v6)]),
    .testTarget(
      name: "IndexingWorkerCoreTests", dependencies: ["IndexingWorkerCore"],
      swiftSettings: [.swiftLanguageMode(.v6)]),
    .testTarget(
      name: "IndexingWorkerHostTests",
      dependencies: [
        "IndexingWorkerCore", "IndexingWorkerHost",
        .product(name: "Logging", package: "swift-log"),
        .product(name: "PostgresNIO", package: "postgres-nio"),
        .product(name: "ThinAppViewCore", package: "thin-app-view-core"),
      ], swiftSettings: [.swiftLanguageMode(.v6)]),
  ]
)
