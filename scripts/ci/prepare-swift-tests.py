#!/usr/bin/env python3
"""Put all backend tests in one SwiftPM plan without changing production settings."""
import argparse
from pathlib import Path
import shutil


def prepare(root: Path) -> None:
    appview = root / "services/appview"
    core = root / "packages/swift/thin-app-view-core"
    destination = appview / ".ci-tests"
    if destination.exists():
        raise ValueError("CI test targets have already been prepared")
    targets = {
        "ReadStateCoreTests": core / "Dependencies/ReadStateCore/Tests/ReadStateCoreTests",
        "ThinAppViewCoreTests": core / "Tests/ThinAppViewCoreTests",
    }
    # Check inputs before changing the service manifest.
    extension = (root / "scripts/ci/appview-test-targets.swift").read_text()
    for source in targets.values():
        if not source.is_dir():
            raise ValueError(f"Missing test sources: {source}")
    # Retain canonical directory depth: some tests locate shared vectors and
    # migration fixtures by walking up from #filePath.
    copied_core = destination / "thin-app-view-core"
    for source in targets.values():
        shutil.copytree(source, copied_core / source.relative_to(core))
    for name in ["TestSupport", "read-state"]:
        if (core / name).is_dir():
            shutil.copytree(core / name, copied_core / name)
    with (appview / "Package.swift").open("a") as manifest:
        manifest.write("\n" + extension)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path)
    prepare(parser.parse_args().root.resolve())
