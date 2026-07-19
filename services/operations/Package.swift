// swift-tools-version: 6.0
import PackageDescription

let package = Package(
  name: "SocialWireOperations",
  platforms: [.macOS(.v14)],
  dependencies: [
    .package(path: "../../packages/swift/GatewayCore"),
    .package(path: "../../packages/swift/OperationsCore"),
    .package(path: "../../packages/swift/ThinAppViewCore"),
    .package(url: "https://github.com/hummingbird-project/hummingbird.git", from: "2.6.0"),
    .package(url: "https://github.com/hummingbird-project/hummingbird-auth.git", from: "2.0.0"),
    .package(url: "https://github.com/swift-server/async-http-client.git", from: "1.23.0"),
    .package(url: "https://github.com/vapor/postgres-nio.git", from: "1.21.0"),
    .package(url: "https://github.com/apple/swift-log.git", from: "1.6.0"),
    .package(url: "https://github.com/apple/swift-argument-parser.git", from: "1.4.0"),
    .package(url: "https://github.com/apple/swift-nio-ssl.git", from: "2.25.0"),
    .package(url: "https://github.com/apple/swift-crypto.git", from: "3.14.0"),
    .package(url: "https://github.com/apple/swift-nio.git", from: "2.80.0"),
  ],
  targets: [
    .executableTarget(
      name: "Operations",
      dependencies: [
        "GatewayCore",
        "OperationsCore",
        "ThinAppViewCore",
        .product(name: "Hummingbird", package: "hummingbird"),
        .product(name: "HummingbirdAuth", package: "hummingbird-auth"),
        .product(name: "AsyncHTTPClient", package: "async-http-client"),
        .product(name: "PostgresNIO", package: "postgres-nio"),
        .product(name: "Logging", package: "swift-log"),
        .product(name: "ArgumentParser", package: "swift-argument-parser"),
        .product(name: "NIOSSL", package: "swift-nio-ssl"),
        .product(name: "Crypto", package: "swift-crypto"),
        .product(name: "NIOCore", package: "swift-nio"),
      ],
      swiftSettings: [
        .swiftLanguageMode(.v6),
        .unsafeFlags(["-warnings-as-errors"]),
      ]
    ),
    .testTarget(
      name: "OperationsTests",
      dependencies: [
        "Operations",
        "OperationsCore",
        "GatewayCore",
        .product(name: "HummingbirdTesting", package: "hummingbird"),
        .product(name: "Logging", package: "swift-log"),
      ],
      swiftSettings: [
        .swiftLanguageMode(.v6),
        .unsafeFlags(["-warnings-as-errors"]),
      ]
    ),
  ]
)
