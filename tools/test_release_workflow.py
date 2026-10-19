#!/usr/bin/env python3
"""Guards the release workflow's fail-closed publication order."""

from __future__ import annotations

import ipaddress
import ast
import re
import sys
from pathlib import Path

try:
    import yaml
except ModuleNotFoundError as error:
    raise SystemExit(
        "release workflow test requires PyYAML; install it with `python -m pip install pyyaml`"
    ) from error


REQUIRED_FRAGMENTS = (
    "persist-credentials: false",
    "Stage release candidate artifacts",
    "SPROUT_RELEASE_UPLOAD_TOKEN",
    "upload_release_artifacts_http.py",
    "upload_release_artifacts.py",
    "Configure SFTP fallback",
    "sftp_configured=false",
    "SFTP fallback is partially configured",
    "SFTP_HOST must be a hostname or IP address",
    "SFTP_USER contains unsupported characters",
    "is not a readable private key",
    "SPROUT_DOWNLOAD_SFTP_KNOWN_HOSTS",
    "Verify staged download candidate",
    "Create immutable GitHub release",
    "Recover and verify GitHub release assets",
    "Verify candidate images before promotion",
    "Promote verified release candidate",
    "docker buildx imagetools inspect",
    "Promote verified images",
    "docker buildx imagetools create",
)

FORBIDDEN_FRAGMENTS = (
    "continue-on-error: true",
    "steps.upload_release.outcome == 'success'",
    "Release $tag already exists; skipping.",
    "existing_release=",
    "publication will verify and backfill assets.",
    "artifact_url.startswith(f\"{base_url}/\")",
    "https://download.clarkhub.cn/releases/{version}/stable/manifest.json",
    "f\"{base_url}/releases/{version}/stable/manifest.json\"",
    "ssh-keyscan",
    "StrictHostKeyChecking=no",
    "StrictHostKeyChecking=accept-new",
)


def step_by_name(workflow: dict[object, object], name: str) -> dict[object, object]:
    steps = workflow["jobs"]["release"]["steps"]
    matches = [
        step
        for step in steps
        if isinstance(step, dict) and step.get("name") == name
    ]
    if len(matches) != 1:
        raise AssertionError(f"expected exactly one workflow step named {name!r}")
    return matches[0]


def parse_pinned_known_hosts(
    document: str,
    host: str,
    port: int,
) -> list[str]:
    """Mimics the workflow's strict known_hosts acceptance contract."""
    entries: list[list[str]] = []
    for line_number, line in enumerate(document.splitlines(), start=1):
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if stripped.startswith(("@", "!", "|")) or "*" in stripped or "?" in stripped:
            raise ValueError(f"marker or wildcard on line {line_number}")
        fields = stripped.split()
        if len(fields) not in (3, 4):
            raise ValueError(f"malformed entry on line {line_number}")
        hosts, key_type, key_value = fields[-3:]
        if any(
            host_name.startswith(("!", "*", "|"))
            or any(character in host_name for character in ("*", "?"))
            for host_name in hosts.split(",")
        ):
            raise ValueError(f"wildcard or hash on line {line_number}")
        if not key_type.startswith(("ssh-", "ecdsa-", "sk-", "rsa-sha2-")):
            raise ValueError(f"unsupported key type on line {line_number}")
        if len(key_value) < 32:
            raise ValueError(f"short key on line {line_number}")
        entries.append(hosts.split(","))
    if not entries:
        raise ValueError("no host key entries")

    normalized_host = host.lower()
    try:
        normalized_host = ipaddress.ip_address(host).compressed.lower()
    except ValueError:
        pass
    expected_hosts = {
        normalized_host,
        f"[{normalized_host}]:{port}" if ":" in normalized_host else f"{normalized_host}:{port}",
    }
    if port == 22:
        expected_hosts.add(f"[{normalized_host}]" if ":" in normalized_host else normalized_host)

    def normalize_known_host(host_name: str) -> str:
        host_name = host_name.rstrip(".").lower()
        if host_name.startswith("[") and "]:" in host_name:
            address_text, port_text = host_name[1:].split("]:", 1)
            try:
                return f"[{ipaddress.ip_address(address_text).compressed}]:{port_text}"
            except ValueError:
                return host_name
        if host_name.startswith("[") and host_name.endswith("]"):
            try:
                return f"[{ipaddress.ip_address(host_name[1:-1]).compressed}]"
            except ValueError:
                return host_name
        return host_name

    provided_hosts = {
        normalize_known_host(host_name)
        for entry in entries
        for host_name in entry
    }
    if not expected_hosts.intersection(provided_hosts):
        raise ValueError("configured host and port are not pinned")
    return sorted(expected_hosts)


def verify_pinned_host_contract() -> None:
    valid_ed25519 = "AAAAC3NzaC1lZDI1NTE5AAAAIL7ri1Q8eP2gVzCjm7A7sQeF1zD0w4mC5q8p6h3k9xYz"
    parse_pinned_known_hosts(
        f"download.example.test ssh-ed25519 {valid_ed25519}\n",
        "download.example.test",
        22,
    )
    parse_pinned_known_hosts(
        f"[2001:0db8::1]:2022 ssh-ed25519 {valid_ed25519}\n",
        "2001:db8::1",
        2022,
    )
    for rejected_document in (
        f"*.example.test ssh-ed25519 {valid_ed25519}\n",
        f"|1|abcdefghijklmnop= ssh-ed25519 {valid_ed25519}\n",
        f"@cert-authority download.example.test ssh-ed25519 {valid_ed25519}\n",
    ):
        try:
            parse_pinned_known_hosts(rejected_document, "download.example.test", 2022)
        except ValueError:
            continue
        raise AssertionError("pinned known_hosts validation accepted an unsafe document")


def verify_embedded_python_contract(workflow: dict[object, object]) -> None:
    for step_name in (
        "Stage release candidate artifacts",
        "Configure SFTP fallback",
    ):
        step = step_by_name(workflow, step_name)
        script = step.get("run")
        if not isinstance(script, str):
            raise AssertionError(f"{step_name} is missing its shell script")
        for match in re.finditer(
            r"<<'(?P<marker>PY)'\r?\n(?P<body>.*?)\r?\n(?P=marker)",
            script,
            re.DOTALL,
        ):
            body = match.group("body")
            try:
                ast.parse(body)
            except SyntaxError as error:
                raise AssertionError(
                    f"{step_name} embedded Python is invalid: {error}"
                ) from error


def verify_production_contract(
    root: Path,
    workflow: dict[object, object],
) -> None:
    release_service = (
        root
        / "services/device_platform/internal/modules/release_store/service/service.go"
    ).read_text(encoding="utf-8")
    match = re.search(
        r"channelPattern\s*=\s*regexp\.MustCompile\(`([^`]+)`\)",
        release_service,
    )
    if match is None:
        raise AssertionError("release store channel pattern is missing")
    channel_pattern = re.compile(match.group(1))
    if not channel_pattern.fullmatch("candidate-123-1"):
        raise AssertionError("release store rejects isolated candidate channels")
    if "artifactDirectoryKind(kind, platform, fileName)" not in release_service:
        raise AssertionError("release store upload path construction changed unexpectedly")

    compose = yaml.safe_load(
        (root / "deploy/cloud/docker-compose.yml").read_text(encoding="utf-8")
    )
    if not isinstance(compose, dict):
        raise AssertionError("cloud Compose file must parse as a mapping")
    services = compose.get("services")
    if not isinstance(services, dict):
        raise AssertionError("cloud Compose file has no services mapping")
    sftp_volumes = services.get("download_ftp", {}).get("volumes", [])
    expected_mount_suffix = "/releases"
    if not any(
        isinstance(volume, str)
        and volume.startswith("sprout_download_data:/home/")
        and volume.endswith(expected_mount_suffix)
        for volume in sftp_volumes
    ):
        raise AssertionError("candidate SFTP path is not rooted in the download volume")
    nginx_volumes = services.get("download_http", {}).get("volumes", [])
    if "sprout_download_data:/srv/releases:ro" not in nginx_volumes:
        raise AssertionError("download HTTP service is not rooted in the download volume")
    nginx = (root / "deploy/cloud/download/nginx/default.conf").read_text(
        encoding="utf-8"
    )
    if "root /srv/releases;" not in nginx:
        raise AssertionError("Nginx public root does not match the download volume")
    if "release_channel>[a-z][a-z0-9._-]*" not in nginx:
        raise AssertionError("Nginx does not expose isolated candidate channels")

    configure = step_by_name(workflow, "Configure SFTP fallback")
    configure_run = configure.get("run")
    if not isinstance(configure_run, str):
        raise AssertionError("SFTP configuration must contain a shell script")
    for fragment in (
        'known_hosts="$RUNNER_TEMP/sprout-sftp/known_hosts"',
        "SPROUT_DOWNLOAD_SFTP_KNOWN_HOSTS contains no host key entries",
        "does not pin the configured SFTP host and port",
        'ssh-keygen -F "$host_query" -f "$known_hosts"',
    ):
        if fragment not in configure_run:
            raise AssertionError(f"SFTP host pinning is missing: {fragment}")

    upload = step_by_name(workflow, "Stage release candidate artifacts")
    upload_run = upload.get("run")
    if not isinstance(upload_run, str):
        raise AssertionError("candidate staging must contain a shell script")
    if '"channel": candidate_channel' not in upload_run:
        raise AssertionError("management upload does not send the candidate channel")
    if 'artifact_root = Path(dist_dir) / "releases" / version / "stable"' not in upload_run:
        raise AssertionError("candidate staging must read the verified stable manifest")
    if 'remote_path = f"releases/{version}/{candidate_channel}/{relative}"' not in upload_run:
        raise AssertionError("SFTP staging does not isolate the candidate channel")
    if 'public_base_url=f"{download_base_url}/{version}/{candidate_channel}"' not in upload_run:
        raise AssertionError("candidate upload cannot verify a persisted response-loss retry")
    if "except uploader.UploadError as error:" not in upload_run:
        raise AssertionError("candidate upload does not handle an existing persisted artifact")
    if 'if "HTTP 409" not in str(error):' not in upload_run:
        raise AssertionError("candidate retry accepts non-conflict upload failures")

    detect = step_by_name(workflow, "Detect version change")
    detect_run = detect.get("run")
    if not isinstance(detect_run, str):
        raise AssertionError("version detection must contain a shell script")
    for fragment in (
        "release_exists=true",
        "release_exists=false",
        "existing GitHub release targets a different commit",
    ):
        if fragment not in detect_run:
            raise AssertionError(f"release recovery detection is missing: {fragment}")
    if "existing GitHub release has no assets" in detect_run:
        raise AssertionError("partial GitHub releases cannot recover their assets")
    if "existing GitHub release is still a draft" in detect_run:
        raise AssertionError("draft GitHub releases are not recoverable")
    verify_release = step_by_name(
        workflow,
        "Recover and verify GitHub release assets",
    )
    verify_release_run = verify_release.get("run")
    if not isinstance(verify_release_run, str):
        raise AssertionError("GitHub release verification must contain a shell script")
    if "gh release upload" not in verify_release_run:
        raise AssertionError("partial GitHub release assets cannot be recovered")

    verify = step_by_name(workflow, "Verify staged download candidate")
    verify_run = verify.get("run")
    if not isinstance(verify_run, str):
        raise AssertionError("candidate verification must contain a shell script")
    if "# candidate verification must compare against the current index" in verify_run:
        raise AssertionError("candidate verification contains an unexpected marker")
    if not re.search(
        r"import hashlib\s*\n\s*import json\s*\n\s*import os",
        verify_run,
    ):
        raise AssertionError("candidate index verification must import hashlib")


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

    verify_pinned_host_contract()

    parsed = yaml.safe_load(workflow)
    if not isinstance(parsed, dict):
        print("release workflow must parse as a YAML mapping", file=sys.stderr)
        return 1
    try:
        verify_embedded_python_contract(parsed)
    except AssertionError as error:
        print(str(error), file=sys.stderr)
        return 1
    checkout = step_by_name(parsed, "Check out repository history")
    checkout_options = checkout.get("with")
    if not isinstance(checkout_options, dict):
        print("checkout must define explicit options", file=sys.stderr)
        return 1
    if checkout_options.get("persist-credentials") is not False:
        print(
            "checkout must disable persisted credentials before third-party builds",
            file=sys.stderr,
        )
        return 1

    verify_production_contract(Path.cwd(), parsed)

    upload_index = workflow.index("Stage release candidate artifacts")
    images_index = workflow.index("Build and publish versioned platform images")
    release_index = workflow.index("Create immutable GitHub release")
    verify_index = workflow.index("Verify staged download candidate")
    assets_index = workflow.index("Recover and verify GitHub release assets")
    inspect_index = workflow.index("Verify candidate images before promotion")
    promote_release_index = workflow.index("Promote verified release candidate")
    latest_index = workflow.rindex("Promote verified images")
    if not (
        images_index
        < upload_index
        < verify_index
        < release_index
        < assets_index
        < inspect_index
        < promote_release_index
        < latest_index
    ):
        print(
            "release workflow must stage, verify downloads, publish GitHub assets, "
            "verify images, then promote the release and latest images",
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
    if 'if [[ -n "${RELEASE_UPLOAD_TOKEN:-}" ]]; then' not in configure_block:
        print(
            "partial SFTP configuration must keep the authenticated management API usable",
            file=sys.stderr,
        )
        return 1
    if (
        'echo "sftp_configured=false" >> "$GITHUB_OUTPUT"' not in configure_block
        or "using the management API only" not in configure_block
    ):
        print(
            "partial SFTP configuration must disable only the fallback",
            file=sys.stderr,
        )
        return 1
    if (
        "SFTP fallback is partially configured and no management API token is available."
        not in configure_block
    ):
        print(
            "partial SFTP configuration must fail closed without the management API",
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

    upload_start = workflow.index("Stage release candidate artifacts")
    verify_download_start = workflow.index("Verify staged download candidate")
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
    if 'stage_via_sftp()' not in upload_block:
        print(
            "release upload must retain the validated SFTP fallback",
            file=sys.stderr,
        )
        return 1
    verify_start = workflow.index("Verify staged download candidate")
    verify_end = workflow.index("Create immutable GitHub release")
    verify_block = workflow[verify_start:verify_end]
    candidate = step_by_name(parsed, "Verify staged download candidate")
    candidate_run = candidate.get("run")
    if not isinstance(candidate_run, str):
        print("candidate verification must contain a shell script", file=sys.stderr)
        return 1
    for required_fragment in (
        'candidate_channel.startswith("candidate-")',
        'f"{base_url}/{version}/{candidate_channel}/"',
        'if len(matches) > 1:',
        "if matches:",
        'if parsed.path.endswith("/"):',
        "if matching_release(before) != matching_release(after):",
        "candidate staging changed the public release index",
        "if received > expected_size:",
        "if declared_size != expected_size:",
        "if remote_digest != artifact[\"sha256\"]:",
    ):
        if required_fragment not in candidate_run:
            print(
                f"candidate verification is missing: {required_fragment}",
                file=sys.stderr,
            )
            return 1
    if "current_index" not in candidate_run:
        print(
            "candidate verification must compare against the current index",
            file=sys.stderr,
        )
        return 1
    if 'current_index="$RUNNER_TEMP/download-index-before-candidate.json"' not in candidate_run:
        print(
            "candidate verification must read the index before touching candidate files",
            file=sys.stderr,
        )
        return 1
    if "curl --fail --silent --show-error --retry 8 --retry-delay 3 \\" not in candidate_run:
        print(
            "candidate verification must retrieve the current index with curl",
            file=sys.stderr,
        )
        return 1
    if "--max-filesize 4194304" not in candidate_run:
        print(
            "candidate verification must bound the initial index download",
            file=sys.stderr,
        )
        return 1
    if "index-refresh" in candidate_run:
        print(
            "candidate verification must not refresh the public index",
            file=sys.stderr,
        )
        return 1
    index_read_position = candidate_run.find(
        'current_index="$RUNNER_TEMP/download-index-before-candidate.json"'
    )

    for forbidden_fragment in (
        'if not artifact_url.startswith(f"{base_url}/")',
        "https://download.clarkhub.cn/releases/{version}/stable/manifest.json",
    ):
        if forbidden_fragment in verify_block:
            print(
                "download verification must parse and compare canonical origins",
                file=sys.stderr,
            )
            return 1
    for required_fragment in (
        "class SafeRedirectHandler(urllib.request.HTTPRedirectHandler)",
        "def validate_download_url(url, label)",
        "expected_origin = (",
        'if any(segment in ("", ".", "..") for segment in segments)',
        "if parsed.username is not None or parsed.password is not None:",
        "MAX_DOWNLOAD_INDEX_BYTES = 4 * 1024 * 1024",
        "while True:",
        "if received > expected_size:",
        "if declared_size != expected_size:",
    ):
        if required_fragment not in verify_block:
            print(
                f"download verification is missing: {required_fragment}",
                file=sys.stderr,
            )
            return 1
    if 'any(character in url for character in ("?", "#", "@", "\\\\", "%"))' in verify_block:
        print(
            "download URL validation must reject userinfo in authority, not every at-sign",
            file=sys.stderr,
        )
        return 1
    if upload_block.count("index-refresh") != 0:
        print(
            "candidate staging must not call the public index refresh endpoint",
            file=sys.stderr,
        )
        return 1
    if "upload_release_artifacts_http as uploader" not in upload_block:
        print(
            "candidate staging must reuse the managed upload request helpers",
            file=sys.stderr,
        )
        return 1
    if "upload_release_artifacts as uploader" not in upload_block:
        print(
            "candidate staging must reuse the SFTP upload helpers",
            file=sys.stderr,
        )
        return 1
    sftp_stage_start = upload_block.index("stage_via_sftp() {")
    sftp_stage_end = upload_block.index(
        "if [[ -z \"${RELEASE_UPLOAD_TOKEN:-}\" ]]",
        sftp_stage_start,
    )
    sftp_stage = upload_block[sftp_stage_start:sftp_stage_end]
    if not re.search(r"(?m)^\s+import hashlib$", sftp_stage):
        print(
            "candidate SFTP staging must import hashlib before hashing artifacts",
            file=sys.stderr,
        )
        return 1
    if "hashlib.sha256(source.read_bytes()).hexdigest()" not in sftp_stage:
        print(
            "candidate SFTP staging must verify existing artifacts by digest",
            file=sys.stderr,
        )
        return 1
    if 'remote_path = f"releases/{version}/{candidate_channel}/{relative}"' not in upload_block:
        print(
            "candidate staging must write only to the isolated candidate channel",
            file=sys.stderr,
        )
        return 1
    if 'remote_manifest =' not in upload_block:
        print(
            "candidate staging must upload its verified isolated manifest",
            file=sys.stderr,
        )
        return 1
    if (
        'remote_manifest = (\n'
        '                  f"releases/{version}/{candidate_channel}/manifest.json"\n'
        '              )'
        not in upload_block
    ):
        print(
            "candidate manifest must remain isolated by version and candidate channel",
            file=sys.stderr,
        )
        return 1
    promote_release = step_by_name(parsed, "Promote verified release candidate")
    promote_release_run = promote_release.get("run")
    if not isinstance(promote_release_run, str):
        print(
            "verified release promotion must contain a shell script",
            file=sys.stderr,
        )
        return 1
    for required_fragment in (
        '--version "${{ steps.detect.outputs.version }}"',
        '--public-base-url "$download_base_url"',
        "--skip-existing",
        "--channel stable",
        "--index-root releases",
        "upload_release_artifacts.py",
        "upload_release_artifacts_http.py",
        "published download index does not expose the verified release",
    ):
        if required_fragment not in promote_release_run:
            print(
                f"verified release promotion is missing: {required_fragment}",
                file=sys.stderr,
            )
            return 1
    if '--version "$candidate_channel"' in promote_release_run:
        print(
            "stable publication must not reuse the isolated candidate version",
            file=sys.stderr,
        )
        return 1
    if '--version "${{ steps.detect.outputs.version }}"' not in promote_release_run:
        print(
            "stable publication must use the released version",
            file=sys.stderr,
        )
        return 1
    verify_start = workflow.index("Verify staged download candidate")
    verify_end = workflow.index("Create immutable GitHub release")
    verify_block = workflow[verify_start:verify_end]
    for required in (
        "canonical HTTPS origin",
        "download redirect target",
        "SafeRedirectHandler",
        "MAX_INDEX_BYTES = 4 * 1024 * 1024",
        "download index is too large",
        "if len(matches) > 1:",
        "if matches:",
        "download index points at an unexpected release manifest",
    ):
        if required not in verify_block:
            print(
                f"download verification is missing: {required}",
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
        'if ! docker buildx imagetools inspect "ghcr.io/$owner/$image:$RELEASE_VERSION"',
    ):
        if required not in promote_block:
            print(
                f"verified image promotion is missing: {required}",
                file=sys.stderr,
            )
            return 1
    if (
        'if docker buildx imagetools inspect '
        '"ghcr.io/$owner/$image:$RELEASE_VERSION"'
        not in inspect_block
    ):
        print(
            "candidate verification must accept an immutable version image on retry",
            file=sys.stderr,
        )
        return 1
    uploader_path = Path("tools/upload_release_artifacts_http.py")
    uploader = uploader_path.read_text(encoding="utf-8")
    if '"overwrite": "false"' not in uploader:
        print("release upload must be non-overwriting", file=sys.stderr)
        return 1
    if "verify_remote_artifact" not in uploader:
        print("release retry must verify an identical existing artifact", file=sys.stderr)
        return 1
    sftp_uploader = Path("tools/upload_release_artifacts.py").read_text(encoding="utf-8")
    if '"StrictHostKeyChecking=yes"' not in sftp_uploader:
        print("SFTP upload must enforce pinned host keys", file=sys.stderr)
        return 1
    if 'f"UserKnownHostsFile={known_hosts}"' not in sftp_uploader:
        print("SFTP upload must use the pinned known_hosts file", file=sys.stderr)
        return 1

    print("release workflow fail-closed publication-order test passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
