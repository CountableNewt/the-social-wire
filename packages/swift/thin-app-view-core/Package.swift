// swift-tools-version: 6.0
import PackageDescription

let package = Package(
  name: "ThinAppViewCore",
  platforms: [
    .macOS(.v14)
  ],
  products: [
    .library(name: "ThinAppViewCore", targets: ["ThinAppViewCore"]),
    .library(name: "ReadStateCore", targets: ["ReadStateCore"]),
  ],
  dependencies: [
    .package(url: "https://github.com/Stygian-Tech/operations-core.git", revision: "a3b7bb328189a423f3e2d2fdd1dcfd4022faddc1"),
    .package(url: "https://github.com/Stygian-Tech/social-wire-redis.git", revision: "b47429ade063d837039a0a144379200bc0487401"),
    .package(url: "https://github.com/swift-server/async-http-client.git", from: "1.23.0"),
    .package(url: "https://github.com/vapor/postgres-nio.git", from: "1.21.0"),
    .package(url: "https://github.com/groue/GRDB.swift.git", from: "7.10.0"),
    .package(url: "https://github.com/apple/swift-log.git", from: "1.6.0"),
    .package(url: "https://github.com/apple/swift-crypto.git", from: "3.14.0"),
    .package(url: "https://github.com/apple/swift-nio-ssl.git", from: "2.25.0"),
    .package(url: "https://github.com/vapor/websocket-kit.git", from: "2.15.0"),
    .package(url: "https://github.com/21-DOT-DEV/swift-secp256k1.git", revision: "bcd6dd8e615ce243eab7097f213c8c4b39305a2e"),
  ],
  targets: [
    .target(
      name: "ReadStateCore",
      dependencies: [.product(name: "Crypto", package: "swift-crypto")],
      path: "Dependencies/ReadStateCore/Sources/ReadStateCore",
      swiftSettings: [.swiftLanguageMode(.v6)]
    ),
    .testTarget(
      name: "ReadStateCoreTests",
      dependencies: ["ReadStateCore"],
      path: "Dependencies/ReadStateCore/Tests/ReadStateCoreTests",
      swiftSettings: [.swiftLanguageMode(.v6)]
    ),
    .target(
      name: "ThinAppViewCore",
      dependencies: [
        .product(name: "OperationsCore", package: "operations-core"),
        "ReadStateCore",
        .product(name: "SocialWireRedis", package: "social-wire-redis"),
        .product(name: "AsyncHTTPClient", package: "async-http-client"),
        .product(name: "PostgresNIO", package: "postgres-nio"),
        .product(name: "GRDB", package: "GRDB.swift"),
        .product(name: "Logging", package: "swift-log"),
        .product(name: "Crypto", package: "swift-crypto"),
        .product(name: "NIOSSL", package: "swift-nio-ssl"),
        .product(name: "WebSocketKit", package: "websocket-kit", condition: .when(platforms: [.linux])),
        .product(name: "P256K", package: "swift-secp256k1"),
      ],
      path: "Sources/ThinAppViewCore",
      swiftSettings: [
        .swiftLanguageMode(.v6),
      ]
    ),
    .testTarget(
      name: "ThinAppViewCoreTests",
      dependencies: [
        "ThinAppViewCore",
        .product(name: "OperationsCore", package: "operations-core"),
        .product(name: "SocialWireRedis", package: "social-wire-redis"),
        .product(name: "GRDB", package: "GRDB.swift"),
        .product(name: "Logging", package: "swift-log"),
      ],
      path: "Tests/ThinAppViewCoreTests",
      resources: [.copy("Fixtures")],
      swiftSettings: [
        .swiftLanguageMode(.v6),
      ]
    ),
  ]
)
