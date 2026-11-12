// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "SkejBackend",
    platforms: [.macOS(.v14)],
    products: [.library(name: "SkejBackend", targets: ["SkejBackend"])],
    dependencies: [
        .package(url: "https://github.com/hummingbird-project/hummingbird.git", from: "2.6.0"),
        .package(url: "https://github.com/apple/swift-crypto.git", from: "3.14.0"),
        .package(url: "https://github.com/apple/swift-log.git", from: "1.6.0"),
        .package(url: "https://github.com/scinfu/SwiftSoup.git", exact: "2.14.3"),
    ],
    targets: [
        .target(
            name: "SkejBackend",
            dependencies: [
                "SkejKit",
                .product(name: "Hummingbird", package: "hummingbird"),
                .product(name: "Crypto", package: "swift-crypto"),
                .product(name: "Logging", package: "swift-log"),
            ]
        ),
        .target(
            name: "SkejKit",
            dependencies: [
                .product(name: "Crypto", package: "swift-crypto"),
                .product(name: "Logging", package: "swift-log"),
                .product(name: "SwiftSoup", package: "SwiftSoup"),
                .target(name: "CSQLite", condition: .when(platforms: [.linux])),
            ]
        ),
        .systemLibrary(name: "CSQLite", providers: [.apt(["libsqlite3-dev"])]),
        .testTarget(name: "SkejKitTests", dependencies: ["SkejKit"], resources: [.copy("Fixtures")]),
        .testTarget(name: "SkejBackendTests", dependencies: [
            "SkejBackend", "SkejKit",
            .product(name: "HummingbirdTesting", package: "hummingbird"),
        ]),
    ]
)
