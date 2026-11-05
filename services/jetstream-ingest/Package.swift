// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "SocialWireIngress",
    platforms: [.macOS(.v14)],
    products: [
        .executable(name: "jetstream-ingest", targets: ["IngressController"]),
        .executable(name: "schema-ready", targets: ["SchemaReady"]),
    ],
    dependencies: [
        .package(url: "https://github.com/vapor/postgres-nio.git", from: "1.21.0"),
        .package(url: "https://github.com/swift-server/async-http-client.git", from: "1.23.0"),
        .package(url: "https://github.com/vapor/websocket-kit.git", from: "2.15.0"),
        .package(url: "https://github.com/hummingbird-project/hummingbird.git", from: "2.6.0"),
        .package(url: "https://github.com/apple/swift-crypto.git", from: "3.14.0"),
        .package(url: "https://github.com/apple/swift-log.git", from: "1.6.0"),
        .package(url: "https://github.com/apple/swift-nio.git", from: "2.86.0"),
        .package(url: "https://github.com/swift-server/swift-service-lifecycle.git", from: "2.5.0"),
        .package(url: "https://github.com/apple/swift-nio-ssl.git", from: "2.25.0"),
    ],
    targets: [
        .systemLibrary(name: "CZstd", pkgConfig: "libzstd", providers: [.apt(["libzstd-dev"]), .brew(["zstd"])]),
        .target(name: "ArchiveChecksums"),
        .target(name: "IngressCore", dependencies: ["CZstd", "ArchiveChecksums", .product(name: "NIOHTTP1", package: "swift-nio"), .product(name: "NIOWebSocket", package: "swift-nio"), .product(name: "NIOSSL", package: "swift-nio-ssl"), .product(name: "NIOConcurrencyHelpers", package: "swift-nio"), .product(name: "NIOPosix", package: "swift-nio"), .product(name: "PostgresNIO", package: "postgres-nio"), .product(name: "AsyncHTTPClient", package: "async-http-client"), .product(name: "WebSocketKit", package: "websocket-kit"), .product(name: "Crypto", package: "swift-crypto"), .product(name: "Logging", package: "swift-log")], swiftSettings: [.swiftLanguageMode(.v6)]),
        .executableTarget(name: "IngressController", dependencies: ["IngressCore", .product(name: "ServiceLifecycle", package: "swift-service-lifecycle"), .product(name: "Hummingbird", package: "hummingbird")], swiftSettings: [.swiftLanguageMode(.v6)]),
        .executableTarget(name: "SchemaReady", dependencies: ["IngressCore"], swiftSettings: [.swiftLanguageMode(.v6)]),
        .testTarget(name: "IngressCoreTests", dependencies: ["IngressCore", .product(name: "NIOPosix", package: "swift-nio"), .product(name: "NIOHTTP1", package: "swift-nio"), .product(name: "NIOConcurrencyHelpers", package: "swift-nio"), .product(name: "AsyncHTTPClient", package: "async-http-client")], resources: [.copy("Fixtures")], swiftSettings: [.swiftLanguageMode(.v6)]),
    ]
)
