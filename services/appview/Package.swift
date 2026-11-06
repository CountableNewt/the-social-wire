// swift-tools-version: 6.0
import PackageDescription

let package = Package(
  name: "SocialWireAppView",
  platforms: [
    .macOS(.v14)
  ],
  dependencies: [
    .package(url: "https://github.com/Stygian-Tech/gateway-core.git", revision: "c442f6a85210056f5c8b12da75e62a7aaa27d9a2"),
    .package(url: "https://github.com/Stygian-Tech/operations-core.git", revision: "a3b7bb328189a423f3e2d2fdd1dcfd4022faddc1"),
    .package(url: "https://github.com/Stygian-Tech/thin-app-view-core.git", revision: "17a2d8c777b69ab471dec98b52232a245901f49a"),
    .package(url: "https://github.com/Stygian-Tech/wire-core.git", revision: "a37340974f2d26d92faa70bfb048ab0681017b28"),
    .package(url: "https://github.com/Stygian-Tech/finance-core.git", revision: "0509d93c44d2958622471e2a452317407e699cc2"),
    .package(url: "https://github.com/Stygian-Tech/sports-core.git", revision: "dab4c7a598265077260feee6589fc09903731084"),
    .package(url: "https://github.com/Stygian-Tech/social-wire-redis.git", revision: "b47429ade063d837039a0a144379200bc0487401"),
    .package(url: "https://github.com/hummingbird-project/hummingbird.git", from: "2.6.0"),
    .package(url: "https://github.com/hummingbird-project/hummingbird-auth.git", from: "2.0.0"),
    .package(url: "https://github.com/vapor/postgres-nio.git", from: "1.21.0"),
    .package(url: "https://github.com/groue/GRDB.swift.git", from: "7.10.0"),
    .package(url: "https://github.com/swift-server/async-http-client.git", from: "1.21.0"),
    .package(url: "https://github.com/apple/swift-log.git", from: "1.6.0"),
    .package(url: "https://github.com/apple/swift-argument-parser.git", from: "1.4.0"),
    .package(url: "https://github.com/apple/swift-nio-ssl.git", from: "2.25.0"),
    .package(url: "https://github.com/apple/swift-crypto.git", from: "3.14.0"),
    .package(url: "https://github.com/soto-project/soto.git", exact: "7.17.0"),
    .package(url: "https://github.com/scinfu/SwiftSoup.git", exact: "2.14.3"),
  ],
  targets: [
    .executableTarget(
      name: "AppView",
      dependencies: [
        .product(name: "ReadStateCore", package: "thin-app-view-core"),
        .product(name: "GatewayCore", package: "gateway-core"),
        .product(name: "OperationsCore", package: "operations-core"),
        .product(name: "ThinAppViewCore", package: "thin-app-view-core"),
        .product(name: "WireCore", package: "wire-core"),
        .product(name: "FinanceCore", package: "finance-core"),
        .product(name: "SportsCore", package: "sports-core"),
        .product(name: "SocialWireRedis", package: "social-wire-redis"),
        .product(name: "Hummingbird", package: "hummingbird"),
        .product(name: "HummingbirdAuth", package: "hummingbird-auth"),
        .product(name: "PostgresNIO", package: "postgres-nio"),
        .product(name: "GRDB", package: "GRDB.swift"),
        .product(name: "AsyncHTTPClient", package: "async-http-client"),
        .product(name: "Logging", package: "swift-log"),
        .product(name: "ArgumentParser", package: "swift-argument-parser"),
        .product(name: "NIOSSL", package: "swift-nio-ssl"),
        .product(name: "Crypto", package: "swift-crypto"),
        .product(name: "SotoS3", package: "soto"),
        .product(name: "SwiftSoup", package: "SwiftSoup"),
      ],
      path: "Sources/AppView",
      swiftSettings: [
        .swiftLanguageMode(.v6),
        .unsafeFlags(["-warnings-as-errors"]),
      ]
    ),
    .testTarget(
      name: "AppViewTests",
      dependencies: [
        "AppView",
        .product(name: "GatewayCore", package: "gateway-core"),
        .product(name: "ThinAppViewCore", package: "thin-app-view-core"),
        .product(name: "WireCore", package: "wire-core"),
        .product(name: "FinanceCore", package: "finance-core"),
        .product(name: "SportsCore", package: "sports-core"),
        .product(name: "SocialWireRedis", package: "social-wire-redis"),
        .product(name: "AsyncHTTPClient", package: "async-http-client"),
        .product(name: "HummingbirdTesting", package: "hummingbird"),
        .product(name: "Logging", package: "swift-log"),
      ],
      path: "Tests/AppViewTests",
      swiftSettings: [
        .swiftLanguageMode(.v6),
        .unsafeFlags(["-warnings-as-errors"]),
      ]
    ),
  ]
)
