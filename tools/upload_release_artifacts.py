#!/usr/bin/env python3
"""Upload release assets over SFTP and atomically publish the release index."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import subprocess
import sys
from pathlib import Path
from typing import Any

TOOLS_DIR = Path(__file__).resolve().parent
if str(TOOLS_DIR.parent) not in sys.path:
    sys.path.insert(0, str(TOOLS_DIR.parent))

from tools.release_artifacts import merge_release_index

VERSION_PATTERN = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
CHANNEL_PATTERN = re.compile(r"^[a-z][a-z0-9._-]*$")
ARTIFACT_PATTERN = re.compile(
    r"^(?P<platform>[a-z][a-z0-9._-]*)/(?P<kind>[a-z][a-z0-9._-]*)/"
    r"(?P<filename>[A-Za-z0-9._+-]+)$"
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Upload release artifacts through SFTP.")
    parser.add_argument("--dist", required=True, type=Path)
    parser.add_argument("--version", required=True)
    parser.add_argument("--channel", default="stable")
    parser.add_argument("--host", required=True)
    parser.add_argument("--port", required=True, type=int)
    parser.add_argument("--user", required=True)
    parser.add_argument("--private-key", required=True, type=Path)
    parser.add_argument("--known-hosts", required=True, type=Path)
    parser.add_argument("--remote-root", required=True, type=Path)
    parser.add_argument("--index-root", required=True, type=Path)
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--public-base-url", required=True)
    parser.add_argument("--title", default="")
    parser.add_argument("--published-at", default="")
    return parser.parse_args()


def run_sftp(
    *,
    batch_file: Path,
    host: str,
    port: int,
    user: str,
    private_key: Path,
    known_hosts: Path,
) -> None:
    command = [
        "sftp",
        "-b",
        str(batch_file),
        "-P",
        str(port),
        "-i",
        str(private_key),
        "-o",
        "BatchMode=yes",
        "-o",
        "StrictHostKeyChecking=yes",
        "-o",
        f"UserKnownHostsFile={known_hosts}",
        "-o",
        "ConnectTimeout=20",
        f"{user}@{host}",
    ]
    completed = subprocess.run(command, check=False, text=True, capture_output=True)
    if completed.returncode != 0:
        detail = completed.stderr.strip() or completed.stdout.strip()
        raise RuntimeError(f"sftp failed with exit code {completed.returncode}: {detail}")


def shell_quote(value: str) -> str:
    return "'" + value.replace("'", "'\"'\"'") + "'"


def load_manifest(manifest_path: Path) -> dict[str, Any]:
    try:
        payload = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise ValueError(f"invalid release manifest: {manifest_path}") from error
    if not isinstance(payload, dict) or not isinstance(payload.get("artifacts"), list):
        raise ValueError(f"release manifest has no artifacts: {manifest_path}")
    return payload


def build_upload_batch(
    manifest: dict[str, Any],
    dist_dir: Path,
    version: str,
    channel: str,
    remote_root: str,
    *,
    manifest_path: Path | None = None,
    verify_existing: bool = False,
    host: str | None = None,
    port: int | None = None,
    user: str | None = None,
    private_key: Path | None = None,
    known_hosts: Path | None = None,
) -> str:
    lines = [
        "-mkdir releases",
        f"-mkdir releases/{version}",
        f"-mkdir releases/{version}/{channel}",
    ]
    uploaded_paths: set[str] = set()
    for artifact in manifest["artifacts"]:
        if not isinstance(artifact, dict):
            raise ValueError("release manifest artifact is not an object")
        relative = build_artifact_relative_path(artifact)
        source_relative_path = artifact.get("relative_path")
        if not isinstance(source_relative_path, str):
            source_relative_path = (
                f"{artifact['platform']}/{artifact['kind']}/{artifact['file_name']}"
            )
        source = dist_dir / "releases" / version / channel / source_relative_path
        if not source.is_file():
            raise FileNotFoundError(f"release artifact is missing: {source}")
        remote_path = f"{remote_root.rstrip('/')}/{version}/{channel}/{relative}"
        lines.append(f"-mkdir {Path(remote_path).parent.as_posix()}")
        if verify_existing:
            if (
                host is None
                or port is None
                or user is None
                or private_key is None
                or known_hosts is None
            ):
                raise ValueError("existing artifact verification requires SFTP settings")
            expected_size = artifact.get("size_bytes")
            expected_sha256 = artifact.get("sha256")
            if (
                not isinstance(expected_size, int)
                or expected_size <= 0
                or not isinstance(expected_sha256, str)
                or len(expected_sha256) != 64
            ):
                raise ValueError("release artifact checksum metadata is invalid")
            if not remote_file_matches(
                host=host,
                port=port,
                user=user,
                private_key=private_key,
                known_hosts=known_hosts,
                remote_path=remote_path,
                expected_size=expected_size,
                expected_sha256=expected_sha256,
            ):
                lines.append(
                    f"put {shell_quote(str(source))} "
                    f"{shell_quote(remote_path)}"
                )
        else:
            lines.append(
                f"put {shell_quote(str(source))} {shell_quote(remote_path)}"
            )
        uploaded_paths.add(relative)

    if not uploaded_paths:
        raise ValueError("release manifest contains no uploadable artifacts")

    if manifest_path is not None:
        remote_manifest = (
            f"{remote_root.rstrip('/')}/{version}/{channel}/manifest.json"
        )
        lines.append(
            f"put {shell_quote(str(manifest_path))} "
            f"{shell_quote(remote_manifest)}"
        )
    return "\n".join(lines) + "\n"


def build_artifact_relative_path(artifact: dict[str, Any]) -> str:
    relative = f"{artifact['platform']}/{artifact['kind']}/{artifact['file_name']}"
    if not ARTIFACT_PATTERN.fullmatch(relative):
        raise ValueError(f"invalid artifact path: {relative!r}")
    return relative


def download_remote_file(
    *,
    host: str,
    port: int,
    user: str,
    private_key: Path,
    known_hosts: Path,
    remote_path: str,
    local_path: Path,
) -> None:
    batch_file = known_hosts.parent / "download-index.batch"
    batch_file.write_text(
        f"get {shell_quote(remote_path)} {shell_quote(str(local_path))}\n",
        encoding="utf-8",
    )
    run_sftp(
        batch_file=batch_file,
        host=host,
        port=port,
        user=user,
        private_key=private_key,
        known_hosts=known_hosts,
    )

def download_optional_index(
    *,
    host: str,
    port: int,
    user: str,
    private_key: Path,
    known_hosts: Path,
    remote_path: str,
    local_path: Path,
) -> None:
    """Download the current index, treating the first publish as empty state."""
    probe_file = known_hosts.parent / "probe-index.batch"
    probe_file.write_text(
        f"ls {shell_quote(remote_path)}\n",
        encoding="utf-8",
    )
    try:
        run_sftp(
            batch_file=probe_file,
            host=host,
            port=port,
            user=user,
            private_key=private_key,
            known_hosts=known_hosts,
        )
    except RuntimeError as error:
        detail = str(error).lower()
        if "no such file" not in detail and "not found" not in detail:
            raise
        # A missing first-publish index is normal. Return with no local file
        # so the caller writes a fresh index.
        local_path.unlink(missing_ok=True)
        return
    finally:
        probe_file.unlink(missing_ok=True)
    download_remote_file(
        host=host,
        port=port,
        user=user,
        private_key=private_key,
        known_hosts=known_hosts,
        remote_path=remote_path,
        local_path=local_path,
    )


def upload_remote_file(
    *,
    host: str,
    port: int,
    user: str,
    private_key: Path,
    known_hosts: Path,
    local_path: Path,
    remote_path: str,
) -> None:
    batch_file = known_hosts.parent / "upload-index.batch"
    batch_file.write_text(
        "\n".join(
            [
                f"-mkdir {Path(remote_path).parent.as_posix()}",
                f"put {shell_quote(str(local_path))} {shell_quote(remote_path + '.incoming')}",
                f"rename {shell_quote(remote_path + '.incoming')} {shell_quote(remote_path)}",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    run_sftp(
        batch_file=batch_file,
        host=host,
        port=port,
        user=user,
        private_key=private_key,
        known_hosts=known_hosts,
    )


def remove_remote_file(
    *,
    host: str,
    port: int,
    user: str,
    private_key: Path,
    known_hosts: Path,
    remote_path: str,
) -> None:
    batch_file = known_hosts.parent / "cleanup-index.batch"
    batch_file.write_text(
        f"rm {shell_quote(remote_path)}\n",
        encoding="utf-8",
    )
    run_sftp(
        batch_file=batch_file,
        host=host,
        port=port,
        user=user,
        private_key=private_key,
        known_hosts=known_hosts,
    )


def remote_file_matches(
    *,
    host: str,
    port: int,
    user: str,
    private_key: Path,
    known_hosts: Path,
    remote_path: str,
    expected_size: int,
    expected_sha256: str,
) -> bool:
    local_file = known_hosts.parent / (
        "verify-" + hashlib.sha256(remote_path.encode("utf-8")).hexdigest()[:16]
    )
    try:
        download_remote_file(
            host=host,
            port=port,
            user=user,
            private_key=private_key,
            known_hosts=known_hosts,
            remote_path=remote_path,
            local_path=local_file,
        )
    except RuntimeError as error:
        detail = str(error).lower()
        if "no such file" in detail or "not found" in detail:
            return False
        raise
    try:
        if local_file.stat().st_size != expected_size:
            return False
        digest = hashlib.sha256()
        with local_file.open("rb") as input_file:
            for chunk in iter(lambda: input_file.read(1024 * 1024), b""):
                digest.update(chunk)
        return digest.hexdigest() == expected_sha256
    finally:
        local_file.unlink(missing_ok=True)


def local_file_matches(
    local_path: Path,
    expected_size: int,
    expected_sha256: str,
) -> bool:
    if not local_path.is_file() or local_path.stat().st_size != expected_size:
        return False
    digest = hashlib.sha256()
    with local_path.open("rb") as input_file:
        for chunk in iter(lambda: input_file.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest() == expected_sha256


def write_index_candidate(
    *,
    local_index_path: Path,
    manifest: dict[str, Any],
    public_base_url: str,
    title: str,
    published_at: str,
) -> None:
    try:
        existing = json.loads(local_index_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        existing = {"releases": [], "schema_version": 1}
    if not isinstance(existing, dict) or not isinstance(existing.get("releases"), list):
        existing = {"releases": [], "schema_version": 1}
    merged = merge_release_index(existing, manifest, public_base_url, title, published_at)
    local_index_path.write_text(
        json.dumps(merged, ensure_ascii=False, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )


def main() -> None:
    args = parse_args()
    if not VERSION_PATTERN.fullmatch(args.version):
        raise ValueError(f"invalid version: {args.version}")
    if not CHANNEL_PATTERN.fullmatch(args.channel):
        raise ValueError(f"invalid channel: {args.channel}")
    if not args.dist.is_dir():
        raise FileNotFoundError(f"distribution directory does not exist: {args.dist}")
    if not args.private_key.is_file():
        raise FileNotFoundError(f"SFTP private key does not exist: {args.private_key}")
    if not args.known_hosts.is_file():
        raise FileNotFoundError(f"known_hosts file does not exist: {args.known_hosts}")

    manifest = load_manifest(args.manifest)
    if manifest.get("version") != args.version or manifest.get("channel") != args.channel:
        raise ValueError("release manifest version or channel does not match the upload")

    batch_file = args.known_hosts.parent / "release-upload.batch"
    artifact_batch = build_upload_batch(
        manifest=manifest,
        dist_dir=args.dist,
        version=args.version,
        channel=args.channel,
        remote_root=args.remote_root.as_posix(),
        manifest_path=args.manifest,
        verify_existing=True,
        host=args.host,
        port=args.port,
        user=args.user,
        private_key=args.private_key,
        known_hosts=args.known_hosts,
    )
    local_index_path = args.known_hosts.parent / "index.json"
    remote_index_path = f"{args.index_root.as_posix().rstrip('/')}/index.json"
    try:
        batch_file.write_text(artifact_batch, encoding="utf-8")
        run_sftp(
            batch_file=batch_file,
            host=args.host,
            port=args.port,
            user=args.user,
            private_key=args.private_key,
            known_hosts=args.known_hosts,
        )
        download_optional_index(
            host=args.host,
            port=args.port,
            user=args.user,
            private_key=args.private_key,
            known_hosts=args.known_hosts,
            remote_path=remote_index_path,
            local_path=local_index_path,
        )
        write_index_candidate(
            local_index_path=local_index_path,
            manifest=manifest,
            public_base_url=args.public_base_url,
            title=args.title,
            published_at=args.published_at,
        )
        try:
            remove_remote_file(
                host=args.host,
                port=args.port,
                user=args.user,
                private_key=args.private_key,
                known_hosts=args.known_hosts,
                remote_path=f"{remote_index_path}.incoming",
            )
        except RuntimeError:
            # A missing temporary index is the expected first-publish state.
            pass
        upload_remote_file(
            host=args.host,
            port=args.port,
            user=args.user,
            private_key=args.private_key,
            known_hosts=args.known_hosts,
            local_path=local_index_path,
            remote_path=remote_index_path,
        )
    finally:
        batch_file.unlink(missing_ok=True)


if __name__ == "__main__":
    main()
