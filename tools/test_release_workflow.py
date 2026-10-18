#!/usr/bin/env python3
"""Guards the release workflow's fail-closed publication order."""

from __future__ import annotations

import sys
from pathlib import Path


REQUIRED_FRAGMENTS = (
    "Publish release artifacts",
    "SPROUT_RELEASE_UPLOAD_TOKEN",
    "upload_release_artifacts_http.py",
    "upload_release_artifacts.py",
    "Configure SFTP fallback",
    "sftp_configured=false",
    "SFTP fallback is partially configured",
    "SFTP_HOST must be a hostname or IP address",
    "SFTP_USER contains unsupported characters",
    "is not a readable private key",
    "Verify published download service",
    "Create immutable GitHub release",
    "Verify GitHub release assets",
    "Verify candidate images before promotion",
    "docker buildx imagetools inspect",
    "Promote verified images",
    "docker buildx imagetools create",
)

FORBIDDEN_FRAGMENTS = (
    "continue-on-error: true",
    "steps.upload_release.outcome == 'success'",
    "Release $tag already exists; skipping.",
    "--clobber",
    "existing_release=",
    "publication will verify and backfill assets.",
)


def main() -> int:
    workflow_path = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(
        ".github/workflows/release.yml"
    )
    workflow = workflow_path.read_text(encoding="utf-8")
    missing = [fragment for fragment in REQUIRED_FRAGMENTS if fragment not in workflow]
    if missing:
        for fragment in missing:
            print(f"missing workflow fragment: {fragment}", file=sys.stderr)
        return 1
    forbidden = [
        fragment for fragment in FORBIDDEN_FRAGMENTS if fragment in workflow
    ]
    if forbidden:
        for fragment in forbidden:
            print(f"forbidden workflow fragment: {fragment}", file=sys.stderr)
        return 1

    upload_index = workflow.index("Publish release artifacts")
    images_index = workflow.index("Build and publish versioned platform images")
    release_index = workflow.index("Create immutable GitHub release")
    verify_index = workflow.index("Verify published download service")
    assets_index = workflow.index("Verify GitHub release assets")
    inspect_index = workflow.index("Verify candidate images before promotion")
    latest_index = workflow.rindex("Promote verified images")
    if not (
        images_index
        < upload_index
        < verify_index
        < release_index
        < assets_index
        < inspect_index
        < latest_index
    ):
        print(
            "release workflow must verify downloads, GitHub assets, and "
            "versioned images before promoting latest",
            file=sys.stderr,
        )
        return 1

    configure_start = workflow.index("Configure SFTP fallback")
    configure_end = workflow.index("Log in to GitHub Container Registry")
    configure_block = workflow[configure_start:configure_end]
    if 'if [[ "$configured" -eq 0 ]]' not in configure_block:
        print(
            "SFTP configuration must allow the zero-variable management API path",
            file=sys.stderr,
        )
        return 1
    if 'if [[ "${#missing[@]}" -ne 0 ]]' not in configure_block:
        print(
            "SFTP configuration must reject partial credentials",
            file=sys.stderr,
        )
        return 1
    if 'if [[ ! "$SFTP_HOST" =~ ^[A-Za-z0-9][A-Za-z0-9._:-]*$ ]]' not in configure_block:
        print(
            "SFTP configuration must reject unsafe host values",
            file=sys.stderr,
        )
        return 1
    if 'if [[ ! "$SFTP_USER" =~ ^[A-Za-z0-9_][A-Za-z0-9._-]*$ ]]' not in configure_block:
        print(
            "SFTP configuration must reject unsafe usernames",
            file=sys.stderr,
        )
        return 1

    upload_start = workflow.index("Publish release artifacts")
    verify_download_start = workflow.index("Verify published download service")
    upload_block = workflow[upload_start:verify_download_start]
    if 'if [[ -z "${RELEASE_UPLOAD_TOKEN:-}" ]]' not in upload_block:
        print(
            "release upload must distinguish an absent management API token",
            file=sys.stderr,
        )
        return 1
    if 'SFTP_CONFIGURED' not in upload_block:
        print(
            "release upload must use the validated SFTP configuration result",
            file=sys.stderr,
        )
        return 1
    if 'Neither SPROUT_RELEASE_UPLOAD_TOKEN nor a complete SFTP fallback is configured.' not in upload_block:
        print(
            "release upload must fail when neither publication path is usable",
            file=sys.stderr,
        )
        return 1
    if 'publish_via_sftp()' not in upload_block:
        print(
            "release upload must retain the validated SFTP fallback",
            file=sys.stderr,
        )
        return 1

    inspect_start = workflow.index("Verify candidate images before promotion")
    latest_start = workflow.index("Promote verified images")
    inspect_block = workflow[inspect_start:latest_start]
    if "if: steps.detect.outputs.should_release == 'true'" not in inspect_block:
        print(
            "versioned image inspection must only run for a requested release",
            file=sys.stderr,
        )
        return 1
    for image in (
        "sprout-device-platform",
        "sprout-voice-gateway",
        "sprout-admin-web",
        "sprout-upgrade-worker",
    ):
        if image not in inspect_block:
            print(
                f"versioned image inspection is missing: {image}",
                file=sys.stderr,
            )
            return 1

    for required in (
        '--tag "ghcr.io/$owner/sprout-device-platform:$CANDIDATE_TAG"',
        '--tag "ghcr.io/$owner/sprout-voice-gateway:$CANDIDATE_TAG"',
        '--tag "ghcr.io/$owner/sprout-admin-web:$CANDIDATE_TAG"',
        '--tag "ghcr.io/$owner/sprout-upgrade-worker:$CANDIDATE_TAG"',
    ):
        if required not in workflow:
            print(f"missing versioned image: {required}", file=sys.stderr)
            return 1
        if required.replace("$CANDIDATE_TAG", "latest") in workflow:
            print(
                f"latest image must not be pushed before verification: {required}",
                file=sys.stderr,
            )
            return 1

    promote_start = workflow.index("Promote verified images")
    promote_block = workflow[promote_start:]
    for required in (
        '--tag "ghcr.io/$owner/$image:$RELEASE_VERSION"',
        '--tag "ghcr.io/$owner/$image:latest"',
    ):
        if required not in promote_block:
            print(
                f"verified image promotion is missing: {required}",
                file=sys.stderr,
            )
            return 1
    uploader_path = Path("tools/upload_release_artifacts_http.py")
    uploader = uploader_path.read_text(encoding="utf-8")
    if '"overwrite": "false"' not in uploader:
        print("release upload must be non-overwriting", file=sys.stderr)
        return 1

    print("release workflow fail-closed publication-order test passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
