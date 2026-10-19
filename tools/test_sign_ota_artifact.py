import base64
import contextlib
import io
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

from tools import sign_ota_artifact


class SignOtaArtifactTest(unittest.TestCase):
    def build_manifest(
        self,
        artifact: Path,
        private_key: Ed25519PrivateKey,
        **overrides: object,
    ) -> dict[str, object]:
        values: dict[str, object] = {
            "artifact": artifact,
            "private_key": private_key,
            "signature_key_id": "ota-test-1",
            "release_id": "00000000-0000-4000-8000-000000000001",
            "firmware_version": "1.2.3",
            "hardware_revision": "s3_n16r8",
            "artifact_key": "1.2.3/stable/esp32_s3/firmware/firmware.bin",
            "artifact_url": (
                "https://download.example.com/releases/"
                "1.2.3/stable/esp32_s3/firmware/firmware.bin"
            ),
            "channel": "stable",
            "min_source_version": "1.0.0",
            "rollback_allowed": True,
            "mandatory": False,
            "release_notes": "test",
            "published_at": "2026-10-17T00:00:00Z",
        }
        values.update(overrides)
        return sign_ota_artifact.build_release_manifest(**values)

    def assert_manifest_signature_valid(
        self,
        manifest: dict[str, object],
        private_key: Ed25519PrivateKey,
    ) -> None:
        signature = base64.b64decode(str(manifest["signature"]), validate=True)
        private_key.public_key().verify(
            signature,
            sign_ota_artifact.canonical_signature_payload(manifest),
        )

    def test_signature_covers_complete_canonical_policy_payload(self) -> None:
        private_key = Ed25519PrivateKey.generate()
        unrelated_private_key = Ed25519PrivateKey.generate()
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            artifact = root / "firmware.bin"
            artifact.write_bytes(b"ota firmware bytes")
            manifest_path = root / "release.json"
            manifest = self.build_manifest(artifact, private_key)
            sign_ota_artifact.atomic_json_write(manifest_path, manifest)

            self.assertEqual(manifest["signature_payload_version"], "2")
            self.assertEqual(manifest["min_source_version"], "1.0.0")
            self.assertEqual(manifest["published_at"], "2026-10-17T00:00:00Z")
            self.assertEqual(manifest["updated_at"], manifest["published_at"])
            self.assertEqual(manifest["signature_status"], "pending")
            self.assertTrue(manifest_path.is_file())
            self.assert_manifest_signature_valid(manifest, private_key)

            payload = sign_ota_artifact.canonical_signature_payload(manifest)
            expected_prefix = (
                "schema_version=2\n"
                "release_id=00000000-0000-4000-8000-000000000001\n"
                "firmware_version=1.2.3\n"
                "hardware_revision=s3_n16r8\n"
                "channel=stable\n"
            )
            self.assertTrue(payload.startswith(expected_prefix.encode("utf-8")))
            self.assertTrue(payload.endswith(b"min_source_version=1.0.0\n"))
            self.assertNotIn(b"published_at=", payload)
            self.assertIn(b"rollback_allowed=true\n", payload)
            self.assertIn(b"mandatory=false\n", payload)

            signature = base64.b64decode(str(manifest["signature"]), validate=True)
            digest = sign_ota_artifact.sha256_file(artifact)
            with self.assertRaises(InvalidSignature):
                private_key.public_key().verify(signature, digest.encode("ascii"))
            with self.assertRaises(InvalidSignature):
                unrelated_private_key.public_key().verify(signature, payload)

    def test_each_policy_field_is_bound_to_the_signature(self) -> None:
        private_key = Ed25519PrivateKey.generate()
        with tempfile.TemporaryDirectory() as temporary_directory:
            artifact = Path(temporary_directory) / "firmware.bin"
            artifact.write_bytes(b"ota firmware bytes")
            manifest = self.build_manifest(artifact, private_key)
            signature = base64.b64decode(str(manifest["signature"]), validate=True)
            tampered_values: tuple[tuple[str, object], ...] = (
                ("release_id", "00000000-0000-4000-8000-000000000002"),
                ("firmware_version", "9.9.9"),
                ("hardware_revision", "other_board"),
                ("channel", "canary"),
                ("artifact_key", "1.2.3/stable/esp32_s3/firmware/other.bin"),
                (
                    "artifact_url",
                    "https://download.example.com/releases/"
                    "1.2.3/stable/esp32_s3/firmware/other.bin",
                ),
                ("sha256", "0" * 64),
                ("size_bytes", 999),
                ("signature_key_id", "ota-test-2"),
                ("rollback_allowed", False),
                ("mandatory", True),
                ("min_source_version", "0.1.0"),
            )
            for field, value in tampered_values:
                with self.subTest(field=field):
                    tampered = dict(manifest)
                    tampered[field] = value
                    with self.assertRaises(InvalidSignature):
                        private_key.public_key().verify(
                            signature,
                            sign_ota_artifact.canonical_signature_payload(tampered),
                        )

    def test_canonical_payload_rejects_unknown_or_incomplete_policy_shape(self) -> None:
        manifest = {
            field: (
                True
                if field in ("rollback_allowed",)
                else False
                if field == "mandatory"
                else 1
                if field == "size_bytes"
                else "value"
            )
            for field in sign_ota_artifact.CANONICAL_SIGNATURE_FIELDS
        }
        payload = sign_ota_artifact.canonical_signature_payload(manifest)
        self.assertTrue(payload.endswith(b"min_source_version=value\n"))

        with self.assertRaises(ValueError):
            sign_ota_artifact.canonical_signature_payload(
                {
                    key: value
                    for key, value in manifest.items()
                    if key != "mandatory"
                }
            )

    def test_artifact_key_rejects_noncanonical_or_traversing_paths(self) -> None:
        invalid_keys = (
            "",
            "/absolute.bin",
            "firmware\\bootloader.bin",
            "firmware//bootloader.bin",
            "firmware/./bootloader.bin",
            "firmware/../bootloader.bin",
            "firmware/",
            "firmware\nbootloader.bin",
        )
        for artifact_key in invalid_keys:
            with self.subTest(artifact_key=artifact_key):
                with self.assertRaises(ValueError):
                    sign_ota_artifact.validate_artifact_key(artifact_key)

    def test_artifact_url_must_match_the_signed_direct_path(self) -> None:
        artifact_key = "firmware/1.2.3.bin"
        valid_urls = (
            "https://download.example.com/firmware/1.2.3.bin",
            "https://download.example.com/releases/firmware/1.2.3.bin",
        )
        for artifact_url in valid_urls:
            with self.subTest(artifact_url=artifact_url):
                self.assertEqual(
                    sign_ota_artifact.validate_artifact_url(
                        artifact_url,
                        artifact_key,
                    ),
                    artifact_url,
                )

        invalid_urls = (
            "http://download.example.com/firmware/1.2.3.bin",
            "https://download.example.com/other/1.2.3.bin",
            "https://download.example.com/other.bin?key=firmware/1.2.3.bin",
            "https://download.example.com/firmware/%31.2.3.bin",
            "https://download.example.com/firmware/../firmware/1.2.3.bin",
            "https://download.example.com/firmware//1.2.3.bin",
            "https://user:password@download.example.com/firmware/1.2.3.bin",
            "https://download.example.com/firmware/1.2.3.bin#fragment",
            "https://download.example.com/firmware/1.2.3.bin#",
            "https://download.example.com/firmware/1.2.3.bin?channel=stable",
            "https://download.example.com/firmware/1.2.3.bin?",
            "HTTPS://download.example.com/firmware/1.2.3.bin",
            "https://download.example.com/firmware/1.2.3.bin/",
            "https://download.example.com/firmware/1.2.3.bin%2fextra",
            "https://download.example.com/firmware/1.2.3.bin\\extra",
            "https://download.example.com/firmware/固件.bin",
            "https://download.example.com/" + "a" * 512 + "/firmware/1.2.3.bin",
        )
        for artifact_url in invalid_urls:
            with self.subTest(artifact_url=artifact_url):
                with self.assertRaises(ValueError):
                    sign_ota_artifact.validate_artifact_url(
                        artifact_url,
                        artifact_key,
                    )

    def test_artifact_key_matches_device_byte_and_character_limits(self) -> None:
        self.assertEqual(
            sign_ota_artifact.validate_artifact_key("firmware/" + "a" * 503),
            "firmware/" + "a" * 503,
        )
        self.assertEqual(
            sign_ota_artifact.validate_artifact_key(
                "firmware/release@2026/1.2.3.bin"
            ),
            "firmware/release@2026/1.2.3.bin",
        )
        invalid_keys = (
            "firmware/" + "a" * 504,
            "firmware/固件.bin",
            "firmware/1.2.3.bin?",
            "firmware/1.2.3.bin#",
            "firmware/1.2.3.bin%20",
        )
        for artifact_key in invalid_keys:
            with self.subTest(artifact_key=artifact_key):
                with self.assertRaises(ValueError):
                    sign_ota_artifact.validate_artifact_key(artifact_key)

    def test_artifact_url_matches_device_512_byte_boundary(self) -> None:
        artifact_key = "firmware/1.2.3.bin"
        prefix = "https://download.example.com/"
        suffix = "/" + artifact_key
        prefix_length = len(prefix)
        suffix_length = len(suffix)
        maximum_length = sign_ota_artifact.MAX_ARTIFACT_URL_BYTES
        self.assertGreaterEqual(
            maximum_length,
            prefix_length + suffix_length,
        )
        maximum_url = (
            prefix
            + "a" * (maximum_length - prefix_length - suffix_length)
            + suffix
        )
        self.assertEqual(len(maximum_url), 512)
        self.assertEqual(
            sign_ota_artifact.validate_artifact_url(
                maximum_url,
                artifact_key,
            ),
            maximum_url,
        )
        at_path_key = "firmware/release@2026/1.2.3.bin"
        at_path_url = (
            "https://download.example.com/releases/"
            "release@2026/" + at_path_key
        )
        self.assertEqual(
            sign_ota_artifact.validate_artifact_url(
                at_path_url,
                at_path_key,
            ),
            at_path_url,
        )
        oversized_url = (
            prefix
            + "a" * (maximum_length - prefix_length - suffix_length + 1)
            + suffix
        )
        self.assertEqual(len(oversized_url), 513)
        with self.assertRaises(ValueError):
            sign_ota_artifact.validate_artifact_url(
                oversized_url,
                artifact_key,
            )

    def test_artifact_size_matches_device_partition_capacity(self) -> None:
        self.assertEqual(
            sign_ota_artifact.MAX_ARTIFACT_SIZE,
            4 * 1024 * 1024,
        )

    def test_build_release_manifest_rejects_artifact_over_four_mib(self) -> None:
        private_key = Ed25519PrivateKey.generate()
        with tempfile.TemporaryDirectory() as temporary_directory:
            artifact = Path(temporary_directory) / "firmware.bin"
            artifact.write_bytes(
                b"x" * (sign_ota_artifact.MAX_ARTIFACT_SIZE + 1)
            )
            with self.assertRaisesRegex(
                ValueError,
                "between 1 and 4194304 bytes",
            ):
                self.build_manifest(artifact, private_key)

    def test_build_release_manifest_requires_minimum_source_version(self) -> None:
        private_key = Ed25519PrivateKey.generate()
        with tempfile.TemporaryDirectory() as temporary_directory:
            artifact = Path(temporary_directory) / "firmware.bin"
            artifact.write_bytes(b"ota firmware bytes")
            for value in ("", None):
                with self.subTest(value=value):
                    with self.assertRaises(ValueError):
                        self.build_manifest(
                            artifact,
                            private_key,
                            min_source_version=value,
                        )

    def test_release_policy_fields_are_validated_before_signing(self) -> None:
        private_key = Ed25519PrivateKey.generate()
        with tempfile.TemporaryDirectory() as temporary_directory:
            artifact = Path(temporary_directory) / "firmware.bin"
            artifact.write_bytes(b"ota firmware bytes")
            invalid_overrides: tuple[dict[str, object], ...] = (
                {"release_id": "contains space"},
                {"release_id": "OTA_RELEASE"},
                {"release_id": "00000000-0000-4000-8000-00000000000A"},
                {"firmware_version": "1.2"},
                {"firmware_version": "01.2.3"},
                {"hardware_revision": "ESP32-S3"},
                {"channel": "beta"},
                {"min_source_version": "latest"},
                {"published_at": "2026-10-17 00:00:00"},
                {"published_at": "2026-10-17T00:00:00+08:00"},
            )
            for overrides in invalid_overrides:
                with self.subTest(overrides=overrides):
                    with self.assertRaises(ValueError):
                        self.build_manifest(artifact, private_key, **overrides)

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
                "00000000-0000-4000-8000-000000000003",
                "--firmware-version",
                "1.2.3",
                "--hardware-revision",
                "s3_n16r8",
                "--artifact-key",
                "bootloader.bin",
                "--artifact-url",
                "https://download.example.com/bootloader.bin",
                "--min-source-version",
                "1.0.0",
                "--output",
                str(output_path),
            ]
            with mock.patch("sys.argv", ["sign_ota_artifact.py", *arguments]):
                with contextlib.redirect_stdout(io.StringIO()):
                    sign_ota_artifact.main()
            payload = json.loads(output_path.read_text(encoding="utf-8"))
            self.assertEqual(payload["signature_status"], "pending")
            self.assertEqual(payload["signature_payload_version"], "2")
            self.assertEqual(payload["size_bytes"], 10)
            self.assert_manifest_signature_valid(payload, private_key)
            with mock.patch("sys.argv", ["sign_ota_artifact.py", *arguments]):
                with self.assertRaises(FileExistsError):
                    sign_ota_artifact.main()

    def test_command_line_executable_publishes_public_key(self) -> None:
        private_key = Ed25519PrivateKey.generate()
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            artifact = root / "firmware.bin"
            key_path = root / "signing.pem"
            output_path = root / "manifest.json"
            artifact.write_bytes(b"firmware")
            key_path.write_bytes(
                private_key.private_bytes(
                    serialization.Encoding.PEM,
                    serialization.PrivateFormat.PKCS8,
                    serialization.NoEncryption(),
                )
            )
            completed = subprocess.run(
                [
                    sys.executable,
                    str(Path(sign_ota_artifact.__file__).resolve()),
                    "--artifact",
                    str(artifact),
                    "--private-key",
                    str(key_path),
                    "--signature-key-id",
                    "release-key-1",
                    "--release-id",
                    "00000000-0000-4000-8000-000000000004",
                    "--firmware-version",
                    "1.2.3",
                    "--hardware-revision",
                    "esp32s3",
                    "--artifact-key",
                    "firmware.bin",
                    "--artifact-url",
                    "https://download.example.com/firmware.bin",
                "--min-source-version",
                "1.0.0",
                "--output",
                    str(output_path),
                ],
                check=True,
                capture_output=True,
                text=True,
            )
            result = json.loads(completed.stdout)
            expected_public_key = base64.b64encode(
                private_key.public_key().public_bytes(
                    serialization.Encoding.Raw,
                    serialization.PublicFormat.Raw,
                )
            ).decode("ascii")
            self.assertEqual(result["public_key"], expected_public_key)
            self.assertEqual(result["manifest"], str(output_path.resolve()))
            self.assertEqual(result["signature_payload_version"], "2")
            self.assertTrue(output_path.is_file())


if __name__ == "__main__":
    unittest.main()
