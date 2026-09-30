# AI Assistance Disclosure:
# Tool: Codex (GPT-5), date: 2026-09-28
# Scope: Added regression coverage for generated-key permissions and failure status.
# Author review: ZI YANG - validated correctness

"""Behavior tests for the local JWT key-generation script."""

import json
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("generate_jwks.py")


class GenerateJWKSBehaviorTest(unittest.TestCase):
    """Checks the script's observable file-permission and error behavior."""

    @staticmethod
    def write_cryptography_stub(directory: Path) -> None:
        """Creates the minimal cryptography API needed to test file behavior."""
        package = directory / "cryptography" / "hazmat" / "primitives"
        (package / "asymmetric").mkdir(parents=True)
        for init_file in (
            directory / "cryptography" / "__init__.py",
            directory / "cryptography" / "hazmat" / "__init__.py",
            package / "__init__.py",
            package / "asymmetric" / "__init__.py",
        ):
            init_file.write_text("", encoding="utf-8")
        (package / "serialization.py").write_text(
            "class Encoding:\n    PEM = object()\n"
            "class PrivateFormat:\n    PKCS8 = object()\n"
            "class NoEncryption:\n    def __call__(self): return self\n"
            "NoEncryption = lambda: object()\n",
            encoding="utf-8",
        )
        (package / "asymmetric" / "rsa.py").write_text(
            "class PrivateKey:\n"
            "    def private_bytes(self, **_): return b'fake private key'\n"
            "def generate_private_key(**_): return PrivateKey()\n",
            encoding="utf-8",
        )

    def run_script(self, dependency_directory: Path, output: Path) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        existing_python_path = env.get("PYTHONPATH", "")
        env["PYTHONPATH"] = str(dependency_directory) + (
            os.pathsep + existing_python_path if existing_python_path else ""
        )
        return subprocess.run(
            [sys.executable, str(SCRIPT), "--out", str(output)],
            capture_output=True,
            check=False,
            env=env,
            text=True,
        )

    def test_restricts_an_existing_output_file(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "keys.json"
            dependency_directory = Path(directory) / "dependencies"
            self.write_cryptography_stub(dependency_directory)
            output.write_text("old key material", encoding="utf-8")
            output.chmod(0o644)

            result = self.run_script(dependency_directory, output)

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(stat.S_IMODE(output.stat().st_mode), 0o600)
            self.assertEqual(len(json.loads(output.read_text(encoding="utf-8"))["keys"]), 2)

    def test_returns_nonzero_when_output_cannot_be_created(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "missing" / "keys.json"
            dependency_directory = Path(directory) / "dependencies"
            self.write_cryptography_stub(dependency_directory)

            result = self.run_script(dependency_directory, output)

            self.assertNotEqual(result.returncode, 0)
            self.assertIn("Error writing keys:", result.stderr)


if __name__ == "__main__":
    unittest.main()
