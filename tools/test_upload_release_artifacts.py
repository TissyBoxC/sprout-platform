import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from tools import upload_release_artifacts


class UploadReleaseArtifactsTest(unittest.TestCase):
    def test_first_publish_starts_from_empty_index(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            local_index = root / "index.json"
            manifest = {
                "version": "0.9.0",
                "channel": "stable",
                "artifacts": [
                    {
                        "platform": "android",
                        "kind": "apk",
                        "file_name": "sprout-parent-app-v0.9.0.apk",
                        "sha256": "a" * 64,
                    }
                ],
            }

            with mock.patch.object(
                upload_release_artifacts,
                "run_sftp",
                side_effect=RuntimeError("remote index: No such file"),
            ):
                upload_release_artifacts.download_optional_index(
                    host="example.com",
                    port=2022,
                    user="sprout-release",
                    private_key=root / "id_ed25519",
                    known_hosts=root / "known_hosts",
                    remote_path="releases/index.json",
                    local_path=local_index,
                )

            upload_release_artifacts.write_index_candidate(
                local_index_path=local_index,
                manifest=manifest,
                public_base_url="https://download.example.com",
                title="测试发布",
                published_at="2026-10-03T00:00:00Z",
            )
            payload = json.loads(local_index.read_text(encoding="utf-8"))

        self.assertEqual(payload["releases"][0]["version"], "0.9.0")

    def test_same_version_channels_both_remain_in_index(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            local_index = root / "index.json"
            stable_manifest = {
                "version": "0.30.0",
                "channel": "stable",
                "artifacts": [
                    {
                        "platform": "android",
                        "kind": "apk",
                        "file_name": "sprout-parent-app-v0.30.0.apk",
                        "sha256": "a" * 64,
                    }
                ],
            }
            beta_manifest = {
                "version": "0.30.0",
                "channel": "beta",
                "artifacts": [
                    {
                        "platform": "android",
                        "kind": "apk",
                        "file_name": "sprout-parent-app-v0.30.0-beta.apk",
                        "sha256": "b" * 64,
                    }
                ],
            }

            upload_release_artifacts.write_index_candidate(
                local_index_path=local_index,
                manifest=stable_manifest,
                public_base_url="https://download.example.com",
                title="稳定版",
                published_at="2026-10-19T00:00:00Z",
            )
            upload_release_artifacts.write_index_candidate(
                local_index_path=local_index,
                manifest=beta_manifest,
                public_base_url="https://download.example.com",
                title="测试版",
                published_at="2026-10-19T00:01:00Z",
            )
            payload = json.loads(local_index.read_text(encoding="utf-8"))

        entries = {
            release["channel"]: release
            for release in payload["releases"]
            if release["version"] == "0.30.0"
        }
        self.assertEqual(set(entries), {"stable", "beta"})
        self.assertEqual(entries["stable"]["artifact_count"], 1)
        self.assertIn("/0.30.0/stable/manifest.json", entries["stable"]["manifest_url"])
        self.assertIn("/0.30.0/beta/manifest.json", entries["beta"]["manifest_url"])

    def test_authentication_failure_is_not_treated_as_missing_index(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            with mock.patch.object(
                upload_release_artifacts,
                "run_sftp",
                side_effect=RuntimeError("Permission denied (publickey)"),
            ):
                with self.assertRaises(RuntimeError):
                    upload_release_artifacts.download_optional_index(
                        host="example.com",
                        port=2022,
                        user="sprout-release",
                        private_key=root / "id_ed25519",
                        known_hosts=root / "known_hosts",
                        remote_path="releases/index.json",
                        local_path=root / "index.json",
                    )

    def test_existing_verified_sftp_artifact_is_not_uploaded_again(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            source = root / "dist/releases/0.9.0/stable/android/apk/app.apk"
            source.parent.mkdir(parents=True)
            source.write_bytes(b"apk bytes")
            manifest = {
                "version": "0.9.0",
                "channel": "stable",
                "artifacts": [
                    {
                        "platform": "android",
                        "kind": "apk",
                        "file_name": "app.apk",
                        "relative_path": "android/apk/app.apk",
                        "size_bytes": len(b"apk bytes"),
                        "sha256": __import__("hashlib").sha256(b"apk bytes").hexdigest(),
                    }
                ],
            }
            with mock.patch.object(
                upload_release_artifacts,
                "remote_file_matches",
                return_value=True,
            ) as verify:
                batch = upload_release_artifacts.build_upload_batch(
                    manifest=manifest,
                    dist_dir=root / "dist",
                    version="0.9.0",
                    channel="stable",
                    remote_root="releases",
                    manifest_path=root / "manifest.json",
                    verify_existing=True,
                    host="example.com",
                    port=2022,
                    user="sprout-release",
                    private_key=root / "id_ed25519",
                    known_hosts=root / "known_hosts",
                )

        self.assertEqual(batch.count("put "), 1)
        self.assertIn("manifest.json'", batch)
        verify.assert_called_once()

    def test_partial_sftp_retry_uploads_only_missing_artifacts(self) -> None:
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            artifacts = []
            for name in ("a.apk", "b.apk"):
                source = root / f"dist/releases/0.9.0/stable/android/apk/{name}"
                source.parent.mkdir(parents=True, exist_ok=True)
                content = name.encode("utf-8")
                source.write_bytes(content)
                artifacts.append(
                    {
                        "platform": "android",
                        "kind": "apk",
                        "file_name": name,
                        "relative_path": f"android/apk/{name}",
                        "size_bytes": len(content),
                        "sha256": __import__("hashlib")
                        .sha256(content)
                        .hexdigest(),
                    }
                )
            manifest = {
                "version": "0.9.0",
                "channel": "stable",
                "artifacts": artifacts,
            }
            with mock.patch.object(
                upload_release_artifacts,
                "remote_file_matches",
                side_effect=[True, False],
            ):
                batch = upload_release_artifacts.build_upload_batch(
                    manifest=manifest,
                    dist_dir=root / "dist",
                    version="0.9.0",
                    channel="stable",
                    remote_root="releases",
                    manifest_path=root / "manifest.json",
                    verify_existing=True,
                    host="example.com",
                    port=2022,
                    user="sprout-release",
                    private_key=root / "id_ed25519",
                    known_hosts=root / "known_hosts",
                )

        self.assertNotIn("a.apk'", batch)
        self.assertIn("b.apk'", batch)
        self.assertEqual(batch.count("put "), 2)
        self.assertIn("manifest.json'", batch)


if __name__ == "__main__":
    unittest.main()
