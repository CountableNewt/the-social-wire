"""Exercise the aggregate release test plan with a dependency-free Swift fixture."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
spec = importlib.util.spec_from_file_location("prepare_swift_tests", HERE / "prepare-swift-tests.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class AggregateSwiftTests(unittest.TestCase):
    def swift(self, root, *args):
        result = subprocess.run(["swift", *args, "--package-path", str(root), "--disable-sandbox", "--build-system", "native"],
                                capture_output=True, text=True, env=os.environ.copy())
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return result.stdout

    @unittest.skipUnless(shutil.which("swift"), "Swift compiler required")
    def test_release_plan_keeps_testability_and_resources(self):
        with tempfile.TemporaryDirectory(prefix="tsw-aggregate-tests-") as temporary:
            root = Path(temporary)
            core = root / "packages/swift/thin-app-view-core"
            app = root / "services/appview"
            (root / "scripts/ci").mkdir(parents=True)
            shutil.copyfile(HERE / "appview-test-targets.swift", root / "scripts/ci/appview-test-targets.swift")
            core.mkdir(parents=True)
            app.mkdir(parents=True)
            # External dependencies in the real extension are represented as local
            # products so fixture execution never fetches packages from the network.
            extension = (root / "scripts/ci/appview-test-targets.swift").read_text()
            for product, identity in [("OperationsCore", "operations-core"), ("SocialWireRedis", "social-wire-redis"), ("GRDB", "GRDB.swift"), ("Logging", "swift-log")]:
                extension = extension.replace(f'      .product(name: "{product}", package: "{identity}"),\n', "")
            (root / "scripts/ci/appview-test-targets.swift").write_text(extension)
            (core / "Package.swift").write_text('''// swift-tools-version: 6.0
import PackageDescription
let package = Package(name: "ThinAppViewCore", platforms: [.macOS("27.0")], products: [
.library(name: "ThinAppViewCore", targets: ["ThinAppViewCore"]),
.library(name: "ReadStateCore", targets: ["ReadStateCore"])
], targets: [.target(name: "ThinAppViewCore"), .target(name: "ReadStateCore")])
''')
            for name in ["ThinAppViewCore", "ReadStateCore"]:
                source = core / "Sources" / name
                source.mkdir(parents=True)
                (source / "Value.swift").write_text("func internalValue() -> Int { 42 }\n")
                target = core / ("Tests/ThinAppViewCoreTests" if name == "ThinAppViewCore" else "Dependencies/ReadStateCore/Tests/ReadStateCoreTests")
                target.mkdir(parents=True)
                body = f'import Testing\n@testable import {name}\n@Test func {name.lower()}Value() {{ #expect(internalValue() == 42) }}\n'
                if name == "ThinAppViewCore":
                    (target / "Fixtures").mkdir()
                    (target / "Fixtures/value.txt").write_text("fixture\n")
                    body += 'import Foundation\n@Test func copiedResource() throws { let url = try #require(Bundle.module.url(forResource: "value", withExtension: "txt", subdirectory: "Fixtures")); #expect(try String(contentsOf: url, encoding: .utf8) == "fixture\\n") }\n'
                (target / "ValueTests.swift").write_text(body)
            # Shared vectors are outside the target directory in the canonical
            # layout. The copied suite must preserve its #filePath lookup depth.
            vectors = core / "read-state/fixtures"
            vectors.mkdir(parents=True)
            (vectors / "value.txt").write_text("shared vector")
            read_test = core / "Dependencies/ReadStateCore/Tests/ReadStateCoreTests/ValueTests.swift"
            with read_test.open("a") as source:
                source.write('import Foundation\n@Test func sharedVector() throws { var root = URL(fileURLWithPath: #filePath); for _ in 0..<5 { root.deleteLastPathComponent() }; #expect(try String(contentsOf: root.appendingPathComponent("read-state/fixtures/value.txt"), encoding: .utf8) == "shared vector") }\n')
            (app / "Package.swift").write_text('''// swift-tools-version: 6.0
import PackageDescription
let package = Package(name: "AppView", platforms: [.macOS("27.0")], dependencies: [.package(path: "../../packages/swift/thin-app-view-core")], targets: [
.executableTarget(name: "AppView"), .testTarget(name: "AppViewTests", dependencies: ["AppView"])
])
''')
            (app / "Sources/AppView").mkdir(parents=True)
            (app / "Sources/AppView/main.swift").write_text('func appInternalValue() -> Int { 7 }\nprint(appInternalValue())\n')
            (app / "Tests/AppViewTests").mkdir(parents=True)
            (app / "Tests/AppViewTests/AppTests.swift").write_text('import Testing\n@testable import AppView\n@Test func appValue() { #expect(appInternalValue() == 7) }\n')
            self.swift(app, "build", "-c", "release", "--product", "AppView")
            module.prepare(root)
            result = self.swift(app, "test", "-c", "release")
            self.assertRegex(result, r"Test run with 5 tests(?: in 0 suites)? passed")
            with self.assertRaisesRegex(ValueError, "already been prepared"):
                module.prepare(root)

    @unittest.skipUnless(shutil.which("swift"), "Swift compiler required")
    def test_aggregate_targets_match_canonical_manifests(self):
        with tempfile.TemporaryDirectory(prefix="tsw-aggregate-manifest-") as temporary:
            root = Path(temporary)
            for path in ["services/appview/Package.swift", "scripts/ci/appview-test-targets.swift"]:
                destination = root / path
                destination.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(ROOT / path, destination)
            for path in ["Tests/ThinAppViewCoreTests", "Dependencies/ReadStateCore/Tests/ReadStateCoreTests"]:
                (root / "packages/swift/thin-app-view-core" / path).mkdir(parents=True)
            module.prepare(root)
            actual = json.loads(self.swift(root / "services/appview", "package", "dump-package"))
            canonical = json.loads(self.swift(ROOT / "packages/swift/thin-app-view-core", "package", "dump-package"))
            actual_targets = {t["name"]: t for t in actual["targets"]}
            for expected in canonical["targets"]:
                if expected["type"] != "test":
                    continue
                expected["path"] = ".ci-tests/thin-app-view-core/" + expected["path"]
                for index, dependency in enumerate(expected["dependencies"]):
                    if "byName" in dependency:
                        expected["dependencies"][index] = {"product": [dependency["byName"][0], "thin-app-view-core", None, None]}
                self.assertEqual(actual_targets[expected["name"]], expected)


if __name__ == "__main__":
    unittest.main()
