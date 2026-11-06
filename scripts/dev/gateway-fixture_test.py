#!/usr/bin/env python3
"""Offline fixture preparation never substitutes an installed Gateway."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

HELPER = Path(__file__).with_name("gateway-fixture.sh").resolve()


class GatewayFixtureTests(unittest.TestCase):
    def run_prepare(self, root, builder):
        executable = root / "bin" / "python3"
        executable.parent.mkdir()
        executable.write_text("#!/bin/bash\n" + builder)
        executable.chmod(0o700)
        env = dict(os.environ, PATH=str(executable.parent) + ":" + os.environ["PATH"],
                   FIXTURE_ROOT=str(root), FIXTURE_HELPER=str(HELPER),
                   JPS_TEST_GATEWAY_BUNDLE="/unrelated/installed")
        return subprocess.run(["bash", "-ec", 'root="$FIXTURE_ROOT/repository"; tmp="$FIXTURE_ROOT/scratch"; . "$FIXTURE_HELPER"; prepare_gateway_fixture; printf "%s\\n" "$JPS_TEST_GATEWAY_BUNDLE"'],
                              env=env, capture_output=True, text=True)

    def test_reuses_prepared_bundle_without_build(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bundle = root / "scratch/dialect-gateway"
            bundle.mkdir(parents=True)
            (bundle / "gateway-bundle.json").write_text("{}")
            result = self.run_prepare(root, "exit 91\n")
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout.strip(), str(bundle))

    def test_missing_bundle_calls_pinned_repository_builder(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            result = self.run_prepare(root, '[ "$1" = "$FIXTURE_ROOT/repository/scripts/build-bundle.py" ] && [ "$2" = --gateway-only ] && [ "$3" = --output ] || exit 92\nmkdir -p "$4"\nprintf "{}" > "$4/gateway-bundle.json"\n')
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout.strip(), str(root / "scratch/dialect-gateway"))

    def test_failed_preparation_never_uses_installed_fallback(self):
        with tempfile.TemporaryDirectory() as directory:
            result = self.run_prepare(Path(directory), "exit 93\n")
            self.assertEqual(result.returncode, 93)
            self.assertEqual(result.stdout, "")


if __name__ == "__main__":
    unittest.main()
