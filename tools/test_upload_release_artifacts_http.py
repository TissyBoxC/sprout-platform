#!/usr/bin/env python3
"""Tests the HTTPS release uploader's request contract."""

from __future__ import annotations

import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from tools import upload_release_artifacts_http


class FakeResponse:
    def __init__(self, payload: dict[str, object]) -> None:
        self.payload = json.dumps(payload).encode("utf-8")

    def read(self) -> bytes:
        return self.payload

    def __enter__(self) -> "FakeResponse":
        return self

    def __exit__(self, *args: object) -> None:
        return None


class UploadReleaseArtifactsHTTPTest(unittest.TestCase):
    def test_multipart_body_contains_structured_release_metadata(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            source = Path(temporary_directory) / "resource.bin"
            source.write_bytes(b"release bytes")

            body, boundary = upload_release_artifacts_http.multipart_body(
                {
                    "version": "0.12.4",
                    "channel": "stable",
                    "platform": "any",
                    "kind": "resource",
                    "filename": "resource.bin",
                    "overwrite": "true",
                },
                source,
            )

            self.assertIn(b"release bytes", body)
            self.assertIn(b'name="version"', body)
            self.assertIn(b"0.12.4", body)
            self.assertIn(b'filename="resource.bin"', body)
            self.assertTrue(boundary)

    def test_main_uploads_every_artifact_then_refreshes_index(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            source = root / "dist/releases/0.12.4/stable/any/resource/resource.bin"
            source.parent.mkdir(parents=True)
            source.write_bytes(b"release bytes")
            manifest_path = root / "manifest.json"
            manifest_path.write_text(
                json.dumps(
                    {
                        "version": "0.12.4",
                        "channel": "stable",
                        "artifacts": [
                            {
                                "platform": "any",
                                "kind": "resource",
                                "file_name": "resource.bin",
                                "relative_path": "any/resource/resource.bin",
                            }
                        ],
                    }
                ),
                encoding="utf-8",
            )
            arguments = [
                "upload_release_artifacts_http.py",
                "--api-base-url",
                "https://api.example.test",
                "--public-base-url",
                "https://download.example.test",
                "--token",
                "t" * 32,
                "--dist",
                str(root / "dist"),
                "--version",
                "0.12.4",
                "--manifest",
                str(manifest_path),
            ]
            requests: list[object] = []

            def fake_urlopen(request: object, timeout: int) -> FakeResponse:
                requests.append((request, timeout))
                return FakeResponse({"data": {}})

            with mock.patch.object(upload_release_artifacts_http.sys, "argv", arguments):
                with mock.patch.object(
                    upload_release_artifacts_http.urllib.request,
                    "urlopen",
                    fake_urlopen,
                ):
                    upload_release_artifacts_http.main()

            self.assertEqual(len(requests), 2)
            upload_request = requests[0][0]
            index_request = requests[1][0]
            self.assertTrue(
                upload_request.full_url.endswith(
                    "/api/v1/release-publication/files"
                )
            )
            self.assertTrue(
                index_request.full_url.endswith(
                    "/api/v1/release-publication/index-refresh"
                )
            )
            self.assertEqual(
                index_request.get_header("Authorization"),
                "Bearer " + "t" * 32,
            )

    def test_main_skips_existing_artifact_after_upload_response_loss(self) -> None:
        content = b"already persisted release bytes"
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            source = (
                root
                / "dist/releases/0.12.4/stable/any/resource/resource.bin"
            )
            source.parent.mkdir(parents=True)
            source.write_bytes(content)
            manifest_path = root / "manifest.json"
            manifest_path.write_text(
                json.dumps(
                    {
                        "version": "0.12.4",
                        "channel": "stable",
                        "artifacts": [
                            {
                                "platform": "any",
                                "kind": "resource",
                                "file_name": "resource.bin",
                                "relative_path": "any/resource/resource.bin",
                                "size_bytes": len(content),
                                "sha256": __import__("hashlib")
                                .sha256(content)
                                .hexdigest(),
                            }
                        ],
                    }
                ),
                encoding="utf-8",
            )
            arguments = [
                "upload_release_artifacts_http.py",
                "--api-base-url",
                "https://api.example.test",
                "--public-base-url",
                "https://download.example.test",
                "--token",
                "t" * 32,
                "--dist",
                str(root / "dist"),
                "--version",
                "0.12.4",
                "--manifest",
                str(manifest_path),
                "--skip-existing",
            ]

            class ExistingArtifactResponse:
                headers = {"Content-Length": str(len(content))}

                def __init__(self) -> None:
                    self._payload = content

                def read(self, size: int) -> bytes:
                    payload, self._payload = self._payload, b""
                    return payload[:size]

                def __enter__(self) -> "ExistingArtifactResponse":
                    return self

                def __exit__(self, *args: object) -> None:
                    return None

            responses = iter(
                [
                    ExistingArtifactResponse(),
                    FakeResponse({"data": {}}),
                ]
            )

            def fake_urlopen(request: object, timeout: int) -> object:
                if str(request.full_url).endswith(
                    "/api/v1/release-publication/files"
                ):
                    error = upload_release_artifacts_http.urllib.error.HTTPError(
                        request.full_url,
                        409,
                        "conflict",
                        {},
                        io.BytesIO(b'{"error":{"code":"file_exists"}}'),
                    )
                    self.addCleanup(error.close)
                    raise error
                return next(responses)

            with mock.patch.object(
                upload_release_artifacts_http.sys,
                "argv",
                arguments,
            ), mock.patch.object(
                upload_release_artifacts_http.urllib.request,
                "urlopen",
                fake_urlopen,
            ):
                upload_release_artifacts_http.main()

    def test_skip_existing_verifies_identical_public_artifact(self) -> None:
        content = b"already published release bytes"
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            source = (
                root
                / "dist/releases/0.12.4/stable/any/resource/resource.bin"
            )
            source.parent.mkdir(parents=True)
            source.write_bytes(content)
            artifact = {
                "platform": "any",
                "kind": "resource",
                "file_name": "resource.bin",
                "relative_path": "any/resource/resource.bin",
                "size_bytes": len(content),
                "sha256": __import__("hashlib").sha256(content).hexdigest(),
            }
            upload_error = upload_release_artifacts_http.UploadError(
                "upload resource.bin failed with HTTP 409: conflict"
            )

            class ExistingArtifactResponse:
                headers = {"Content-Length": str(len(content))}

                def __init__(self) -> None:
                    self._payload = content

                def read(self, size: int) -> bytes:
                    payload, self._payload = self._payload, b""
                    return payload[:size]

                def __enter__(self) -> "ExistingArtifactResponse":
                    return self

                def __exit__(self, *args: object) -> None:
                    return None

            with mock.patch.object(
                upload_release_artifacts_http,
                "request_json",
                side_effect=upload_error,
            ), mock.patch.object(
                upload_release_artifacts_http.urllib.request,
                "urlopen",
                return_value=ExistingArtifactResponse(),
            ) as public_request:
                uploaded = upload_release_artifacts_http.upload_artifact(
                    api_base_url="https://api.example.test",
                    token="t" * 32,
                    dist_dir=root / "dist",
                    version="0.12.4",
                    channel="stable",
                    artifact=artifact,
                    public_base_url="https://download.example.test",
                    skip_existing=True,
                )

            self.assertFalse(uploaded)
            self.assertEqual(
                public_request.call_args.args[0].full_url,
                "https://download.example.test/0.12.4/stable/"
                "any/resource/resource.bin",
            )

    def test_skip_existing_rejects_different_public_artifact(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            source = (
                root
                / "dist/releases/0.12.4/stable/any/resource/resource.bin"
            )
            source.parent.mkdir(parents=True)
            source.write_bytes(b"expected release bytes")

            class DifferentArtifactResponse:
                headers = {}

                def read(self, size: int) -> bytes:
                    if getattr(self, "_read", False):
                        return b""
                    self._read = True
                    return b"different release bytes"[1:]

                def __enter__(self) -> "DifferentArtifactResponse":
                    return self

                def __exit__(self, *args: object) -> None:
                    return None

            with mock.patch.object(
                upload_release_artifacts_http.urllib.request,
                "urlopen",
                return_value=DifferentArtifactResponse(),
            ):
                with self.assertRaisesRegex(
                    upload_release_artifacts_http.UploadError,
                    "checksum",
                ):
                    upload_release_artifacts_http.verify_remote_artifact(
                        public_base_url="https://download.example.test",
                        version="0.12.4",
                        channel="stable",
                        relative_path="any/resource/resource.bin",
                        source=source,
                        artifact={},
                    )


if __name__ == "__main__":
    unittest.main()
