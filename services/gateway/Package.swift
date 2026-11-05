// swift-tools-version: 6.0
import PackageDescription

let package = Package(
  name: "SocialWireGateway",
  platforms: [
    .macOS(.v14)
  ],
  dependencies: [
    .package(url: "https://github.com/Stygian-Tech/gateway-core.git", revision: "228e17fa3b576f61a999c376d2193d78d1122ad8"),
    .package(url: "https://github.com/Stygian-Tech/operations-core.git", revision: "a3b7bb328189a423f3e2d2fdd1dcfd4022faddc1"),
    .package(url: "https://github.com/Stygian-Tech/thin-app-view-core.git", revision: "ffa868f979283cff6ce207e0dbb422ff2ea45136"),
    .package(url: "https://github.com/Stygian-Tech/social-wire-redis.git", revision: "313305b98919ac8075313044b6d867c13650a329"),
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
