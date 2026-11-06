// swift-tools-version: 6.0
import PackageDescription

let package = Package(
  name: "SocialWireSchemaReady",
  platforms: [.macOS(.v14)],
  products: [.executable(name: "schema-ready", targets: ["SchemaReady"])],
  dependencies: [
    .package(url: "https://github.com/vapor/postgres-nio.git", from: "1.21.0")
  ],
  targets: [
    .target(
      name: "SchemaReadyCore",
      dependencies: [.product(name: "PostgresNIO", package: "postgres-nio")],
      swiftSettings: [.swiftLanguageMode(.v6)]),
    .executableTarget(
      name: "SchemaReady", dependencies: ["SchemaReadyCore"],
      swiftSettings: [.swiftLanguageMode(.v6)]),
    .testTarget(
      name: "SchemaReadyCoreTests", dependencies: ["SchemaReadyCore"],
      swiftSettings: [.swiftLanguageMode(.v6)]),
  ]
)
