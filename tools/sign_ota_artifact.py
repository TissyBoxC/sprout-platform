#!/usr/bin/env python3
"""Sign a complete OTA manifest policy payload and emit a release manifest."""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import re
import sys
import uuid
from collections.abc import Mapping
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from cryptography.exceptions import UnsupportedAlgorithm
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import (
    Ed25519PrivateKey,
    Ed25519PublicKey,
)

from tools.release_artifacts import atomic_json_write

SIGNATURE_KEY_ID_PATTERN = re.compile(r"^[A-Za-z0-9._:-]{1,64}$")
SEMVER_PATTERN = re.compile(
    r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$"
)
HARDWARE_REVISION_PATTERN = re.compile(r"^[a-z0-9][a-z0-9_-]{0,31}$")
CHANNEL_PATTERN = re.compile(r"^(?:stable|canary|internal)$")
MAX_ARTIFACT_SIZE = 4 * 1024 * 1024
MAX_ARTIFACT_KEY_BYTES = 512
MAX_ARTIFACT_URL_BYTES = 512
SIGNATURE_PAYLOAD_VERSION = "2"
CANONICAL_SIGNATURE_FIELDS = (
    "schema_version",
    "release_id",
    "firmware_version",
    "hardware_revision",
    "channel",
    "artifact_key",
    "artifact_url",
    "sha256",
    "size_bytes",
    "signature_key_id",
    "signature_algorithm",
    "rollback_allowed",
    "mandatory",
    "min_source_version",
)
REQUIRED_SIGNATURE_POLICY_FIELDS = frozenset(CANONICAL_SIGNATURE_FIELDS)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Sign the complete canonical OTA policy payload with an Ed25519 "
            "private key."
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
    parser.add_argument("--min-source-version", required=True)
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


def validate_artifact_key(artifact_key: str) -> str:
    """Return a canonical relative POSIX artifact path or reject it."""
    if not isinstance(artifact_key, str) or not artifact_key:
        raise ValueError("artifact key must be a non-empty relative POSIX path")
    if len(artifact_key.encode("utf-8")) > MAX_ARTIFACT_KEY_BYTES:
        raise ValueError("artifact key must not exceed 512 UTF-8 bytes")
    if "\\" in artifact_key or "\x00" in artifact_key:
        raise ValueError("artifact key must not contain backslashes or NUL")
    if any(ord(character) < 0x21 or ord(character) > 0x7E for character in artifact_key):
        raise ValueError("artifact key must use printable ASCII characters")
    if any(character in artifact_key for character in ("?", "#", "%")):
        raise ValueError("artifact key must not contain query, fragment, or escape delimiters")
    if artifact_key.startswith("/"):
        raise ValueError("artifact key must be relative")
    segments = artifact_key.split("/")
    if any(segment in ("", ".", "..") for segment in segments):
        raise ValueError("artifact key must not contain empty, dot, or dot-dot segments")
    if "\r" in artifact_key or "\n" in artifact_key:
        raise ValueError("artifact key must not contain line breaks")
    return artifact_key


def _canonical_url_path_segments(path: str) -> tuple[str, ...]:
    if not path.startswith("/") or path == "/" or path.endswith("/"):
        raise ValueError("artifact URL path must identify a file")
    raw_segments = path[1:].split("/")
    if any(segment in ("", ".", "..") for segment in raw_segments):
        raise ValueError("artifact URL path contains an empty or traversal segment")
    if any("\\" in segment or "%" in segment for segment in raw_segments):
        raise ValueError("artifact URL path must use direct, unescaped path segments")
    return tuple(raw_segments)


def validate_artifact_url(artifact_url: str, artifact_key: str) -> str:
    """Require an HTTPS URL whose direct path ends with the artifact key."""
    validate_artifact_key(artifact_key)
    if not isinstance(artifact_url, str) or not artifact_url:
        raise ValueError("artifact URL must be a non-empty HTTPS URL")
    if len(artifact_url.encode("utf-8")) > MAX_ARTIFACT_URL_BYTES:
        raise ValueError("artifact URL must not exceed 512 UTF-8 bytes")
    if any(character in artifact_url for character in ("\r", "\n", "\x00")):
        raise ValueError("artifact URL must not contain line breaks or NUL")
    if any(ord(character) < 0x21 or ord(character) > 0x7E for character in artifact_url):
        raise ValueError("artifact URL must use printable ASCII characters")
    if any(character in artifact_url for character in ("?", "#", "%", "\\")):
        raise ValueError("artifact URL must not contain query, fragment, escape, or backslash characters")
    try:
        parsed = urlsplit(artifact_url)
        parsed_port = parsed.port
    except ValueError as error:
        raise ValueError("artifact URL is malformed") from error
    if not artifact_url.startswith("https://"):
        raise ValueError("artifact URL must use a lowercase https:// scheme")
    if parsed.scheme != "https" or not parsed.hostname:
        raise ValueError("artifact URL must use HTTPS with a host")
    if parsed.username is not None or parsed.password is not None:
        raise ValueError("artifact URL must not contain user information")
    if parsed.query:
        raise ValueError("artifact URL must not contain a query")
    if parsed.fragment:
        raise ValueError("artifact URL must not contain a fragment")
    if parsed_port is not None and not 1 <= parsed_port <= 65535:
        raise ValueError("artifact URL port is out of range")

    key_segments = tuple(artifact_key.split("/"))
    path_segments = _canonical_url_path_segments(parsed.path)
    if len(path_segments) < len(key_segments):
        raise ValueError("artifact URL path does not contain the artifact key")
    if path_segments[-len(key_segments) :] != key_segments:
        raise ValueError("artifact URL path does not contain the artifact key")
    if (
        len(path_segments) > len(key_segments)
        and not artifact_url.endswith("/" + artifact_key)
    ):
        raise ValueError("artifact URL must end directly with the artifact key")
    return artifact_url


def _canonical_value(field: str, value: Any) -> str:
    if field in ("rollback_allowed", "mandatory"):
        if not isinstance(value, bool):
            raise ValueError(f"{field} must be a boolean")
        return "true" if value else "false"
    if field == "size_bytes":
        if isinstance(value, bool) or not isinstance(value, int) or value < 1:
            raise ValueError("size_bytes must be a positive integer")
        return str(value)
    if not isinstance(value, str):
        raise ValueError(f"{field} must be a string")
    if "\r" in value or "\n" in value or "\x00" in value:
        raise ValueError(f"{field} must not contain line breaks or NUL")
    return value


def canonical_signature_payload(manifest: Mapping[str, Any]) -> bytes:
    """Build the exact UTF-8 signature payload shared with firmware."""
    missing_fields = REQUIRED_SIGNATURE_POLICY_FIELDS.difference(manifest)
    if missing_fields:
        raise ValueError(
            "manifest is missing canonical fields: "
            + ", ".join(sorted(missing_fields))
        )
    lines: list[str] = []
    for field in CANONICAL_SIGNATURE_FIELDS:
        if field == "schema_version":
            value = SIGNATURE_PAYLOAD_VERSION
        else:
            value = _canonical_value(field, manifest[field])
        lines.append(f"{field}={value}\n")
    return "".join(lines).encode("utf-8")


def validate_release_policy_fields(
    *,
    release_id: str,
    firmware_version: str,
    hardware_revision: str,
    channel: str,
    min_source_version: str,
    published_at: str,
) -> None:
    string_fields = {
        "release id": release_id,
        "firmware version": firmware_version,
        "hardware revision": hardware_revision,
        "channel": channel,
        "minimum source version": min_source_version,
        "published_at": published_at,
    }
    for field_name, value in string_fields.items():
        if not isinstance(value, str) or not value:
            raise ValueError(f"{field_name} must be a non-empty string")
    try:
        parsed_release_id = uuid.UUID(release_id)
    except (AttributeError, ValueError) as error:
        raise ValueError("release id must be a canonical UUID") from error
    if str(parsed_release_id) != release_id:
        raise ValueError("release id must be a lowercase canonical UUID")
    if not SEMVER_PATTERN.fullmatch(firmware_version):
        raise ValueError("firmware version must be a semantic version")
    if not HARDWARE_REVISION_PATTERN.fullmatch(hardware_revision):
        raise ValueError("hardware revision must match ^[a-z0-9][a-z0-9_-]{0,31}$")
    if not CHANNEL_PATTERN.fullmatch(channel):
        raise ValueError("channel must be stable, canary, or internal")
    if not SEMVER_PATTERN.fullmatch(min_source_version):
        raise ValueError("minimum source version must be a semantic version")
    if not re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z", published_at):
        raise ValueError("published_at must be a UTC RFC 3339 timestamp ending in Z")


def sign_signature_payload(
    private_key: Ed25519PrivateKey,
    payload: bytes,
) -> str:
    if not isinstance(payload, bytes) or not payload.endswith(b"\n"):
        raise ValueError("signature payload must be UTF-8 lines with a final newline")
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
        raise ValueError("OTA artifact size must be between 1 and 4194304 bytes")
    timestamp = published_at or datetime.now(timezone.utc).strftime(
        "%Y-%m-%dT%H:%M:%SZ"
    )
    minimum_source = min_source_version
    validate_release_policy_fields(
        release_id=release_id,
        firmware_version=firmware_version,
        hardware_revision=hardware_revision,
        channel=channel,
        min_source_version=minimum_source,
        published_at=timestamp,
    )
    validate_artifact_key(artifact_key)
    validate_artifact_url(artifact_url, artifact_key)
    digest = sha256_file(artifact)
    manifest: dict[str, Any] = {
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
        "signature_payload_version": SIGNATURE_PAYLOAD_VERSION,
        "signature": "",
        "signature_status": "pending",
        "rollback_allowed": rollback_allowed,
        "mandatory": mandatory,
        "min_source_version": minimum_source,
        "release_notes": release_notes,
        "created_at": timestamp,
        "updated_at": timestamp,
        "published_at": timestamp,
    }
    manifest["signature"] = sign_signature_payload(
        private_key,
        canonical_signature_payload(manifest),
    )
    return manifest


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
                "signature_payload_version": SIGNATURE_PAYLOAD_VERSION,
            },
            indent=2,
            sort_keys=True,
        )
    )


if __name__ == "__main__":
    main()
