#!/usr/bin/env python3
"""Publish release artifacts through the device-platform internal API.

The release workflow uses this path when the management API is reachable over
HTTPS. It keeps the SFTP uploader as an offline fallback and never sends raw
credentials into logs.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import mimetypes
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any


class UploadError(RuntimeError):
    """Raised when the management API rejects a release publication."""


# Large mobile artifacts are tens of megabytes; a short socket timeout drops
# the connection mid-upload on slow or proxied links. Use a generous timeout
# and retry transient network failures instead of failing the whole release.
REQUEST_TIMEOUT_SECONDS = 900
MAX_ATTEMPTS = 4
RETRY_BACKOFF_SECONDS = 5
RETRYABLE_STATUS = frozenset({408, 425, 429, 500, 502, 503, 504})


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Upload release artifacts through the internal management API."
    )
    parser.add_argument("--api-base-url", required=True)
    parser.add_argument("--token", required=True)
    parser.add_argument("--dist", required=True, type=Path)
    parser.add_argument("--version", required=True)
    parser.add_argument("--channel", default="stable")
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--public-base-url", required=True)
    parser.add_argument("--title", default="")
    parser.add_argument("--published-at", default="")
    parser.add_argument(
        "--skip-existing",
        action="store_true",
        help="verify and skip an identical artifact already present on a retry",
    )
    return parser.parse_args()


def load_manifest(manifest_path: Path) -> dict[str, Any]:
    try:
        payload = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise UploadError(f"invalid release manifest: {manifest_path}") from error
    if not isinstance(payload, dict) or not isinstance(payload.get("artifacts"), list):
        raise UploadError(f"release manifest has no artifacts: {manifest_path}")
    return payload


def multipart_body(
    fields: dict[str, str],
    file_path: Path,
) -> tuple[bytes, str]:
    boundary = "----sprout-release-" + hashlib.sha256(
        str(file_path).encode("utf-8")
    ).hexdigest()[:24]
    chunks: list[bytes] = []
    for name, value in fields.items():
        chunks.append(f"--{boundary}\r\n".encode("utf-8"))
        chunks.append(
            f'Content-Disposition: form-data; name="{name}"\r\n\r\n'.encode("utf-8")
        )
        chunks.append(value.encode("utf-8"))
        chunks.append(b"\r\n")
    chunks.append(f"--{boundary}\r\n".encode("utf-8"))
    filename = file_path.name.replace('"', "")
    content_type = mimetypes.guess_type(filename)[0] or "application/octet-stream"
    chunks.append(
        (
            f'Content-Disposition: form-data; name="file"; filename="{filename}"\r\n'
            f"Content-Type: {content_type}\r\n\r\n"
        ).encode("utf-8")
    )
    chunks.append(file_path.read_bytes())
    chunks.append(b"\r\n")
    chunks.append(f"--{boundary}--\r\n".encode("utf-8"))
    return b"".join(chunks), boundary


def request_json(
    request: urllib.request.Request,
    operation: str,
) -> dict[str, Any]:
    last_error: Exception | None = None
    for attempt in range(1, MAX_ATTEMPTS + 1):
        try:
            with urllib.request.urlopen(
                request,
                timeout=REQUEST_TIMEOUT_SECONDS,
            ) as response:
                payload = response.read()
            break
        except urllib.error.HTTPError as error:
            detail = error.read().decode("utf-8", errors="replace")[:1000]
            if error.code not in RETRYABLE_STATUS:
                raise UploadError(
                    f"{operation} failed with HTTP {error.code}: {detail}"
                ) from error
            last_error = UploadError(
                f"{operation} failed with HTTP {error.code}: {detail}"
            )
        except (urllib.error.URLError, TimeoutError, OSError) as error:
            reason = getattr(error, "reason", error)
            last_error = UploadError(
                f"{operation} network error: {reason}"
            )
        if attempt < MAX_ATTEMPTS:
            delay = RETRY_BACKOFF_SECONDS * attempt
            print(
                f"{operation} attempt {attempt} failed "
                f"({last_error}); retrying in {delay}s",
                file=sys.stderr,
            )
            time.sleep(delay)
    else:
        raise last_error if last_error is not None else UploadError(
            f"{operation} failed after {MAX_ATTEMPTS} attempts"
        )
    try:
        decoded = json.loads(payload)
    except json.JSONDecodeError as error:
        raise UploadError(f"{operation} returned invalid JSON") from error
    if not isinstance(decoded, dict):
        raise UploadError(f"{operation} returned an invalid response object")
    return decoded


def upload_artifact(
    *,
    api_base_url: str,
    token: str,
    dist_dir: Path,
    version: str,
    channel: str,
    artifact: dict[str, Any],
    public_base_url: str,
    skip_existing: bool,
) -> bool:
    platform = str(artifact["platform"])
    kind = str(artifact["kind"])
    file_name = str(artifact["file_name"])
    relative_path = str(artifact["relative_path"])
    source = dist_dir / "releases" / version / channel / relative_path
    if not source.is_file():
        raise UploadError(f"release artifact is missing: {source}")
    body, boundary = multipart_body(
        {
            "version": version,
            "channel": channel,
            "platform": platform,
            "kind": kind,
            "filename": file_name,
            "overwrite": "false",
        },
        source,
    )
    request = urllib.request.Request(
        urllib.parse.urljoin(
            api_base_url.rstrip("/") + "/",
            "api/v1/release-publication/files",
        ),
        data=body,
        method="POST",
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": f"multipart/form-data; boundary={boundary}",
            "Content-Length": str(len(body)),
        },
    )
    try:
        request_json(request, f"upload {file_name}")
    except UploadError as error:
        if not skip_existing or "HTTP 409" not in str(error):
            raise
        verify_remote_artifact(
            public_base_url=public_base_url,
            version=version,
            channel=channel,
            relative_path=relative_path,
            source=source,
            artifact=artifact,
        )
        print(f"verified existing {file_name}")
        return False
    return True


def verify_remote_artifact(
    *,
    public_base_url: str,
    version: str,
    channel: str,
    relative_path: str,
    source: Path,
    artifact: dict[str, Any],
) -> None:
    expected_size = artifact.get("size_bytes")
    expected_sha256 = artifact.get("sha256")
    if expected_size is None:
        expected_size = source.stat().st_size
    if expected_sha256 is None:
        expected_sha256 = hashlib.sha256(source.read_bytes()).hexdigest()
    if (
        not isinstance(expected_size, int)
        or isinstance(expected_size, bool)
        or expected_size <= 0
        or not isinstance(expected_sha256, str)
        or len(expected_sha256) != 64
    ):
        raise UploadError("existing artifact verification metadata is invalid")
    url = (
        public_base_url.rstrip("/")
        + "/"
        + urllib.parse.quote(version, safe="")
        + "/"
        + urllib.parse.quote(channel, safe="")
        + "/"
        + urllib.parse.quote(relative_path, safe="/._-~!$&()*+,;=:@")
    )
    request = urllib.request.Request(url, method="GET")
    last_error: Exception | None = None
    for attempt in range(1, MAX_ATTEMPTS + 1):
        try:
            with urllib.request.urlopen(
                request,
                timeout=REQUEST_TIMEOUT_SECONDS,
            ) as response:
                content_length = response.headers.get("Content-Length")
                if content_length is not None:
                    try:
                        declared_size = int(content_length)
                    except ValueError as error:
                        raise UploadError(
                            "existing artifact has an invalid Content-Length"
                        ) from error
                    if declared_size != expected_size:
                        raise UploadError(
                            "existing artifact size does not match the manifest"
                        )
                digest = hashlib.sha256()
                received = 0
                while True:
                    chunk = response.read(1024 * 1024)
                    if not chunk:
                        break
                    received += len(chunk)
                    if received > expected_size:
                        raise UploadError(
                            "existing artifact exceeds its manifest size"
                        )
                    digest.update(chunk)
            if (
                received != expected_size
                or digest.hexdigest() != expected_sha256
            ):
                raise UploadError(
                    "existing artifact checksum or size does not match"
                )
            return
        except UploadError:
            raise
        except (urllib.error.URLError, TimeoutError, OSError) as error:
            reason = getattr(error, "reason", error)
            last_error = UploadError(
                f"verify existing artifact network error: {reason}"
            )
        if attempt < MAX_ATTEMPTS:
            delay = RETRY_BACKOFF_SECONDS * attempt
            print(
                f"verify existing artifact attempt {attempt} failed "
                f"({last_error}); retrying in {delay}s",
                file=sys.stderr,
            )
            time.sleep(delay)
    raise last_error if last_error is not None else UploadError(
        "failed to verify an existing artifact"
    )


def refresh_index(
    *,
    api_base_url: str,
    token: str,
    version: str,
    channel: str,
    title: str,
    published_at: str,
) -> None:
    payload = json.dumps(
        {
            "version": version,
            "channel": channel,
            "title": title,
            "published_at": published_at,
        },
        ensure_ascii=False,
    ).encode("utf-8")
    request = urllib.request.Request(
        urllib.parse.urljoin(
            api_base_url.rstrip("/") + "/",
            "api/v1/release-publication/index-refresh",
        ),
        data=payload,
        method="POST",
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
        },
    )
    request_json(request, "refresh release index")


def main() -> None:
    args = parse_args()
    manifest = load_manifest(args.manifest)
    if manifest.get("version") != args.version or manifest.get("channel") != args.channel:
        raise UploadError("release manifest version or channel does not match the upload")
    artifacts = manifest.get("artifacts")
    if not artifacts:
        raise UploadError("release manifest contains no artifacts")
    uploaded_count = 0
    skipped_count = 0
    for artifact in artifacts:
        if not isinstance(artifact, dict):
            raise UploadError("release manifest artifact is not an object")
        uploaded = upload_artifact(
            api_base_url=args.api_base_url,
            token=args.token,
            dist_dir=args.dist,
            version=args.version,
            channel=args.channel,
            artifact=artifact,
            public_base_url=args.public_base_url,
            skip_existing=args.skip_existing,
        )
        if uploaded:
            uploaded_count += 1
        else:
            skipped_count += 1
    refresh_index(
        api_base_url=args.api_base_url,
        token=args.token,
        version=args.version,
        channel=args.channel,
        title=args.title,
        published_at=args.published_at,
    )
    print(
        f"published {uploaded_count} release artifacts; "
        f"verified {skipped_count} existing artifacts"
    )


if __name__ == "__main__":
    try:
        main()
    except UploadError as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1) from error
