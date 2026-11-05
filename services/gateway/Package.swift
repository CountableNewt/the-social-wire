// swift-tools-version: 6.0
import PackageDescription

let package = Package(
  name: "SocialWireGateway",
  platforms: [
    .macOS(.v14)
  ],
  dependencies: [
    .package(url: "https://github.com/Stygian-Tech/gateway-core.git", revision: "5c8c85d62bcb120d97c19008d4edefccd0f245cb"),
    .package(url: "https://github.com/Stygian-Tech/operations-core.git", revision: "a3b7bb328189a423f3e2d2fdd1dcfd4022faddc1"),
    .package(url: "https://github.com/Stygian-Tech/thin-app-view-core.git", revision: "3a5fbe44741fffb1c4ec8a81529f179a7df78f64"),
    .package(url: "https://github.com/Stygian-Tech/social-wire-redis.git", revision: "b47429ade063d837039a0a144379200bc0487401"),
    .package(url: "https://github.com/hummingbird-project/hummingbird.git", from: "2.6.0"),
    .package(url: "https://github.com/hummingbird-project/hummingbird-auth.git", from: "2.0.0"),
    .package(url: "https://github.com/vapor/postgres-nio.git", from: "1.21.0"),
    .package(url: "https://github.com/groue/GRDB.swift.git", from: "7.10.0"),
    .package(url: "https://github.com/swift-server/async-http-client.git", from: "1.21.0"),
    .package(url: "https://github.com/apple/swift-log.git", from: "1.6.0"),
    .package(url: "https://github.com/apple/swift-argument-parser.git", from: "1.4.0"),
    .package(url: "https://github.com/apple/swift-nio-ssl.git", from: "2.25.0"),
  ],
  targets: [
    .executableTarget(
      name: "Gateway",
      dependencies: [
        .product(name: "GatewayCore", package: "gateway-core"),
        .product(name: "OperationsCore", package: "operations-core"),
        .product(name: "ThinAppViewCore", package: "thin-app-view-core"),
        .product(name: "SocialWireRedis", package: "social-wire-redis"),
        .product(name: "Hummingbird", package: "hummingbird"),
        .product(name: "HummingbirdAuth", package: "hummingbird-auth"),
        .product(name: "PostgresNIO", package: "postgres-nio"),
        .product(name: "GRDB", package: "GRDB.swift"),
        .product(name: "AsyncHTTPClient", package: "async-http-client"),
        .product(name: "Logging", package: "swift-log"),
        .product(name: "ArgumentParser", package: "swift-argument-parser"),
        .product(name: "NIOSSL", package: "swift-nio-ssl"),
      ],
      path: "Sources/Gateway",
      swiftSettings: [
        .swiftLanguageMode(.v6),
        .unsafeFlags(["-warnings-as-errors"]),
      ]
    ),
    .testTarget(
      name: "GatewayTests",
      dependencies: [
        "Gateway",
        .product(name: "GatewayCore", package: "gateway-core"),
        .product(name: "ThinAppViewCore", package: "thin-app-view-core"),
        .product(name: "PostgresNIO", package: "postgres-nio"),
        .product(name: "SocialWireRedis", package: "social-wire-redis"),
        .product(name: "AsyncHTTPClient", package: "async-http-client"),
        .product(name: "HummingbirdTesting", package: "hummingbird"),
        .product(name: "Logging", package: "swift-log"),
      ],
      path: "Tests/GatewayTests",
      swiftSettings: [
        .swiftLanguageMode(.v6),
        .unsafeFlags(["-warnings-as-errors"]),
      ]
    ),
  ]
)
