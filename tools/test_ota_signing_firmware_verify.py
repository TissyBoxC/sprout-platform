#!/usr/bin/env python3
"""Verify a manifest produced by the OTA signing tool with firmware C code."""

from __future__ import annotations

import base64
import json
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

from tools.sign_ota_artifact import build_release_manifest

WORKSPACE_ROOT = Path(__file__).resolve().parents[1]
FIRMWARE_ROOT = WORKSPACE_ROOT / "firmware"
OTA_VALIDATE_ROOT = FIRMWARE_ROOT / "components" / "ota_validate"
ED25519_ROOT = FIRMWARE_ROOT / "components" / "ed25519_verify"
FIRMWARE_TEST_SOURCE = (
    OTA_VALIDATE_ROOT / "test" / "test_signed_manifest_integration.cpp"
)


def find_compiler(name: str) -> str:
    compiler = shutil.which(name)
    if compiler is None:
        raise unittest.SkipTest(f"host compiler {name} is unavailable")
    return compiler


def encode_c_string(value: str) -> str:
    escaped = (
        value.replace("\\", "\\\\")
        .replace('"', '\\"')
        .replace("\n", "\\n")
        .replace("\r", "\\r")
    )
    chunks = [escaped[index : index + 100] for index in range(0, len(escaped), 100)]
    if not chunks:
        return '""'
    return "\n".join(f'"{chunk}"' for chunk in chunks)


class OtaSigningFirmwareVerifyTest(unittest.TestCase):
    def test_python_signed_manifest_is_accepted_by_firmware(self) -> None:
        cxx = find_compiler("g++")
        cc = find_compiler("gcc")
        private_key = Ed25519PrivateKey.generate()
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            artifact = root / "firmware.bin"
            artifact.write_bytes(b"real OTA firmware payload")
            manifest = build_release_manifest(
                artifact=artifact,
                private_key=private_key,
                signature_key_id="release-key-1",
                release_id="rel_2026_10_17_e2e",
                firmware_version="1.2.3",
                hardware_revision="esp32s3",
                artifact_key="firmware/1.2.3.bin",
                artifact_url="https://download.example.com/firmware/1.2.3.bin",
                channel="stable",
                min_source_version="0.9.0",
                rollback_allowed=True,
                mandatory=False,
                release_notes="跨语言 OTA 验签",
                published_at="2026-10-17T00:00:00Z",
            )
            public_key = base64.b64encode(
                private_key.public_key().public_bytes(
                    serialization.Encoding.Raw,
                    serialization.PublicFormat.Raw,
                )
            ).decode("ascii")
            keyring = json.dumps(
                {"release-key-1": public_key},
                separators=(",", ":"),
                sort_keys=True,
            )
            fixture = root / "manifest_fixture.cpp"
            fixture.write_text(
                "\n".join(
                    (
                        'extern "C" const char *const sprout_test_release_id =',
                        f"{encode_c_string(str(manifest['release_id']))};",
                        'extern "C" const char *const sprout_test_firmware_version =',
                        f"{encode_c_string(str(manifest['firmware_version']))};",
                        'extern "C" const char *const sprout_test_hardware_revision =',
                        f"{encode_c_string(str(manifest['hardware_revision']))};",
                        'extern "C" const char *const sprout_test_channel =',
                        f"{encode_c_string(str(manifest['channel']))};",
                        'extern "C" const char *const sprout_test_artifact_url =',
                        f"{encode_c_string(str(manifest['artifact_url']))};",
                        'extern "C" const char *const sprout_test_sha256 =',
                        f"{encode_c_string(str(manifest['sha256']))};",
                        'extern "C" const unsigned long long sprout_test_size_bytes =',
                        f"{manifest['size_bytes']}ULL;",
                        'extern "C" const char *const sprout_test_signature_key_id =',
                        f"{encode_c_string(str(manifest['signature_key_id']))};",
                        'extern "C" const char *const sprout_test_signature_algorithm =',
                        f"{encode_c_string(str(manifest['signature_algorithm']))};",
                        'extern "C" const char *const sprout_test_signature =',
                        f"{encode_c_string(str(manifest['signature']))};",
                        'extern "C" const char *const sprout_test_published_at =',
                        f"{encode_c_string(str(manifest['updated_at']))};",
                        'extern "C" const char *const sprout_test_min_source_version =',
                        f"{encode_c_string(str(manifest['min_source_version']))};",
                        'extern "C" const char *const sprout_test_keyring =',
                        f"{encode_c_string(keyring)};",
                    )
                )
                + "\n",
                encoding="utf-8",
            )
            executable = root / "ota-firmware-verify.exe"
            c_sources = (
                OTA_VALIDATE_ROOT / "src" / "ota_manifest.c",
                ED25519_ROOT / "src" / "ed25519_verify.c",
                ED25519_ROOT / "src" / "monocypher.c",
                ED25519_ROOT / "src" / "monocypher-ed25519.c",
            )
            includes = (
                f"-I{OTA_VALIDATE_ROOT / 'include'}",
                f"-I{ED25519_ROOT / 'include'}",
                f"-I{ED25519_ROOT / 'src'}",
            )
            object_files: list[Path] = []
            for source in c_sources:
                object_file = root / f"{source.stem}.o"
                subprocess.run(
                    [
                        cc,
                        "-std=c11",
                        "-Wall",
                        "-Wextra",
                        "-Werror",
                        *includes,
                        "-c",
                        str(source),
                        "-o",
                        str(object_file),
                    ],
                    check=True,
                )
                object_files.append(object_file)
            subprocess.run(
                [
                    cxx,
                    "-std=c++17",
                    "-Wall",
                    "-Wextra",
                    "-Werror",
                    *includes,
                    str(FIRMWARE_TEST_SOURCE),
                    str(fixture),
                    *[str(path) for path in object_files],
                    "-o",
                    str(executable),
                ],
                check=True,
            )
            completed = subprocess.run(
                [str(executable)],
                capture_output=True,
                text=True,
            )
            self.assertEqual(
                completed.returncode,
                0,
                completed.stderr or completed.stdout,
            )
            self.assertIn(
                "signed firmware manifest verification passed",
                completed.stdout,
            )


if __name__ == "__main__":
    unittest.main()
