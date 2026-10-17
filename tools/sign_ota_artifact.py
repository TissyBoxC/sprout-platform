#!/usr/bin/env python3
"""Sign an OTA artifact digest and emit a release manifest fragment."""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import re
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from cryptography.exceptions import UnsupportedAlgorithm
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import (
    Ed25519PrivateKey,
    Ed25519PublicKey,
)

from tools.release_artifacts import atomic_json_write

SIGNATURE_KEY_ID_PATTERN = re.compile(r"^[A-Za-z0-9._:-]{1,64}$")
MAX_ARTIFACT_SIZE = 16 * 1024 * 1024


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Sign the lowercase SHA-256 hex digest of an OTA artifact with "
            "an Ed25519 private key."
        )
    )
    parser.add_argument("--artifact", required=True, type=Path)
    parser.add_argument("--private-key", required=True, type=Path)
    parser.add_argument("--signature-key-id", required=True)
    parser.add_argument("--release-id", required=True)
    parser.add_argument("--firmware-version", required=True)
    parser.add_argument("--hardware-revision", required=True)
    parser.add_argument("--artifact-key", required=True)
    parser.add_argument("--artifact-url", required=True)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--channel", default="stable")
    parser.add_argument("--min-source-version", default="")
    parser.add_argument("--no-rollback", action="store_true")
    parser.add_argument("--mandatory", action="store_true")
    parser.add_argument("--release-notes", default="")
    parser.add_argument("--published-at", default="")
    parser.add_argument("--force", action="store_true")
    return parser.parse_args()


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as input_file:
        for chunk in iter(lambda: input_file.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def load_ed25519_private_key(path: Path) -> Ed25519PrivateKey:
    encoded = path.read_bytes()
    try:
        private_key = serialization.load_pem_private_key(encoded, password=None)
    except (TypeError, ValueError, UnsupportedAlgorithm) as error:
        raise ValueError("private key must be an unencrypted PEM Ed25519 key") from error
    if not isinstance(private_key, Ed25519PrivateKey):
        raise ValueError("private key must use Ed25519")
    return private_key


def sign_digest(private_key: Ed25519PrivateKey, digest: str) -> str:
    if not re.fullmatch(r"[0-9a-f]{64}", digest):
        raise ValueError("digest must be 64 lowercase hexadecimal characters")
    payload = digest.encode("ascii")
    signature = private_key.sign(payload)
    private_key.public_key().verify(signature, payload)
    return base64.b64encode(signature).decode("ascii")


def build_release_manifest(
    *,
    artifact: Path,
    private_key: Ed25519PrivateKey,
    signature_key_id: str,
    release_id: str,
    firmware_version: str,
    hardware_revision: str,
    artifact_key: str,
    artifact_url: str,
    channel: str,
    min_source_version: str,
    rollback_allowed: bool,
    mandatory: bool,
    release_notes: str,
    published_at: str,
) -> dict[str, Any]:
    if not SIGNATURE_KEY_ID_PATTERN.fullmatch(signature_key_id):
        raise ValueError("signature key id must match ^[A-Za-z0-9._:-]{1,64}$")
    if not artifact.is_file():
        raise FileNotFoundError(f"OTA artifact does not exist: {artifact}")
    size_bytes = artifact.stat().st_size
    if size_bytes < 1 or size_bytes > MAX_ARTIFACT_SIZE:
        raise ValueError("OTA artifact size must be between 1 and 16777216 bytes")
    if not artifact_url.startswith("https://"):
        raise ValueError("artifact URL must use HTTPS")
    digest = sha256_file(artifact)
    signature = sign_digest(private_key, digest)
    timestamp = published_at or datetime.now(timezone.utc).isoformat().replace(
        "+00:00", "Z"
    )
    return {
        "schema_version": "1.0.0",
        "release_id": release_id,
        "firmware_version": firmware_version,
        "hardware_revision": hardware_revision,
        "channel": channel,
        "status": "draft",
        "artifact_key": artifact_key,
        "artifact_url": artifact_url,
        "sha256": digest,
        "size_bytes": size_bytes,
        "signature_key_id": signature_key_id,
        "signature_algorithm": "ed25519",
        "signature": signature,
        "signature_status": "pending",
        "rollback_allowed": rollback_allowed,
        "mandatory": mandatory,
        "min_source_version": min_source_version,
        "release_notes": release_notes,
        "created_at": timestamp,
        "updated_at": timestamp,
    }


def public_key_for_cli(private_key: Ed25519PrivateKey) -> str:
    public_key: Ed25519PublicKey = private_key.public_key()
    return base64.b64encode(
        public_key.public_bytes(
            serialization.Encoding.Raw,
            serialization.PublicFormat.Raw,
        )
    ).decode("ascii")


def main() -> None:
    args = parse_args()
    manifest_path = args.output.resolve()
    if args.output.exists() and not args.force:
        raise FileExistsError(
            f"refusing to overwrite existing manifest: {args.output}; pass --force"
        )
    private_key = load_ed25519_private_key(args.private_key)
    manifest = build_release_manifest(
        artifact=args.artifact.resolve(),
        private_key=private_key,
        signature_key_id=args.signature_key_id,
        release_id=args.release_id,
        firmware_version=args.firmware_version,
        hardware_revision=args.hardware_revision,
        artifact_key=args.artifact_key,
        artifact_url=args.artifact_url,
        channel=args.channel,
        min_source_version=args.min_source_version,
        rollback_allowed=not args.no_rollback,
        mandatory=args.mandatory,
        release_notes=args.release_notes,
        published_at=args.published_at,
    )
    atomic_json_write(manifest_path, manifest)
    print(
        json.dumps(
            {
                "manifest": str(manifest_path),
                "public_key": public_key_for_cli(private_key),
                "sha256": manifest["sha256"],
            },
            indent=2,
            sort_keys=True,
        )
    )


if __name__ == "__main__":
    main()
