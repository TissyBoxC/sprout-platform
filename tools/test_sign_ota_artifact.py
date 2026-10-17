import base64
import contextlib
import io
import json
import tempfile
import unittest
from unittest import mock
from pathlib import Path

from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

from tools import sign_ota_artifact


class SignOtaArtifactTest(unittest.TestCase):
    def test_signature_is_valid_with_independent_public_key(self) -> None:
        private_key = Ed25519PrivateKey.generate()
        unrelated_private_key = Ed25519PrivateKey.generate()
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            artifact = root / "firmware.bin"
            artifact.write_bytes(b"ota firmware bytes")
            manifest_path = root / "release.json"

            manifest = sign_ota_artifact.build_release_manifest(
                artifact=artifact,
                private_key=private_key,
                signature_key_id="ota-test-1",
                release_id="ota_release_test",
                firmware_version="1.2.3",
                hardware_revision="s3_n16r8",
                artifact_key="1.2.3/stable/esp32_s3/firmware/firmware.bin",
                artifact_url="https://download.example.com/firmware.bin",
                channel="stable",
                min_source_version="1.0.0",
                rollback_allowed=True,
                mandatory=False,
                release_notes="test",
                published_at="2026-10-17T00:00:00Z",
            )
            sign_ota_artifact.atomic_json_write(manifest_path, manifest)

            digest = sign_ota_artifact.sha256_file(artifact)
            signature = base64.b64decode(manifest["signature"], validate=True)
            public_key = private_key.public_key()
            public_key.verify(signature, digest.encode("ascii"))
            unrelated_public_key = unrelated_private_key.public_key()
            with self.assertRaises(InvalidSignature):
                unrelated_public_key.verify(signature, digest.encode("ascii"))
            self.assertEqual(manifest["sha256"], digest)
            self.assertEqual(manifest["size_bytes"], artifact.stat().st_size)
            self.assertEqual(manifest["signature_status"], "pending")
            self.assertTrue(manifest_path.is_file())

    def test_private_key_must_be_ed25519(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            key_path = Path(temporary_directory) / "wrong.pem"
            key_path.write_bytes(
                b"-----BEGIN PRIVATE KEY-----\nnot-a-key\n-----END PRIVATE KEY-----\n"
            )
            with self.assertRaises(ValueError):
                sign_ota_artifact.load_ed25519_private_key(key_path)

    def test_command_line_writes_manifest_and_validates_existing_file_guard(self) -> None:
        private_key = Ed25519PrivateKey.generate()
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            artifact = root / "bootloader.bin"
            key_path = root / "signing.pem"
            output_path = root / "manifest.json"
            artifact.write_bytes(b"bootloader")
            key_path.write_bytes(
                private_key.private_bytes(
                    serialization.Encoding.PEM,
                    serialization.PrivateFormat.PKCS8,
                    serialization.NoEncryption(),
                )
            )
            arguments = [
                "--artifact",
                str(artifact),
                "--private-key",
                str(key_path),
                "--signature-key-id",
                "ota-test-1",
                "--release-id",
                "ota_release_cli",
                "--firmware-version",
                "1.2.3",
                "--hardware-revision",
                "s3_n16r8",
                "--artifact-key",
                "bootloader.bin",
                "--artifact-url",
                "https://download.example.com/bootloader.bin",
                "--output",
                str(output_path),
            ]
            with mock.patch("sys.argv", ["sign_ota_artifact.py", *arguments]):
                with contextlib.redirect_stdout(io.StringIO()):
                    sign_ota_artifact.main()
            payload = json.loads(output_path.read_text(encoding="utf-8"))
            self.assertEqual(payload["signature_status"], "pending")
            self.assertEqual(payload["size_bytes"], 10)
            with mock.patch("sys.argv", ["sign_ota_artifact.py", *arguments]):
                with self.assertRaises(FileExistsError):
                    sign_ota_artifact.main()


if __name__ == "__main__":
    unittest.main()
