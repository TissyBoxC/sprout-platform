#!/usr/bin/env bash
set -euo pipefail

worker="${1:?worker script}"
repo_root="${2:?repository root}"

fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT

state="$fixture/state/var/lib/sprout-upgrades"
mkdir -p "$state/queue" "$state/processing" "$state/results" "$state/.release-cache"
mkdir -p "$repo_root/deploy/cloud/scripts"
printf '%s\n' '#!/usr/bin/env bash' 'exit 0' \
  > "$repo_root/deploy/cloud/scripts/upgrade-service.sh"
chmod +x "$repo_root/deploy/cloud/scripts/upgrade-service.sh"
cat > "$state/releases.json" <<'JSON'
{
  "generated_at": "2026-10-17T00:00:00Z",
  "services": {
    "device_platform": [],
    "voice_gateway": [],
    "admin_web": [],
    "sub2api": []
  }
}
JSON
cat > "$state/status.json" <<'JSON'
{"generated_at":"2026-10-17T00:00:00Z","services":[]}
JSON

cat > "$fixture/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [ "$1" = "inspect" ]; then
  printf '%s\n' "ghcr.io/tissyboxc/sprout-device-platform:0.23.0"
fi
exit 0
EOF
cat > "$fixture/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
exit 28
EOF
chmod +x "$fixture/docker" "$fixture/curl"

export PATH="$fixture:$PATH"
export SPROUT_SERVICE_VERSION_STATE_DIR="$state"
export SPROUT_SERVICE_UPGRADE_WORKER_RUNTIME_COPY="$fixture/runtime"
cp "$worker" "$SPROUT_SERVICE_UPGRADE_WORKER_RUNTIME_COPY"
export SPROUT_UPGRADE_CLOUD_DIR="$repo_root/deploy/cloud"
export SPROUT_SERVICE_VERSION_CLOUD_DIR="$repo_root/deploy/cloud"
export SPROUT_SERVICE_VERSION_HOST_CLOUD_DIR="$repo_root/deploy/cloud"
export SPROUT_SERVICE_VERSION_POLL_INTERVAL_SECONDS=1
export SPROUT_SERVICE_VERSION_CHECK_INTERVAL_SECONDS=10
export SPROUT_SERVICE_VERSION_RELEASE_CACHE_SECONDS=300
export SPROUT_SERVICE_VERSION_RELEASE_CATALOG_RETRY_SECONDS=5
export SPROUT_SERVICE_VERSION_GITHUB_CONNECT_TIMEOUT_SECONDS=1
export SPROUT_SERVICE_VERSION_GITHUB_REQUEST_TIMEOUT_SECONDS=5
export SPROUT_SERVICE_VERSION_RELEASE_CATALOG_LIMIT=10
export SPROUT_SERVICE_VERSION_STATE_UID=65532
export SPROUT_SERVICE_VERSION_STATE_GID=65532

bash "$worker" >"$fixture/worker.log" 2>&1 &
worker_pid=$!
cleanup() {
  kill "$worker_pid" 2>/dev/null || true
  wait "$worker_pid" 2>/dev/null || true
}
trap 'cleanup; rm -rf "$fixture"' EXIT

first="$state/status.json"
initial="$(jq -r .generated_at "$first")"
sleep 8
after="$(jq -r .generated_at "$first")"
if [ "$after" = "$initial" ]; then
  echo "status snapshot was not refreshed while GitHub was unavailable" >&2
  cat "$fixture/worker.log" >&2
  exit 1
fi
if ! jq -e '.services | length > 0' "$first" >/dev/null; then
  echo "status snapshot lost services while GitHub was unavailable" >&2
  exit 1
fi
if ! jq -e '[.services[] | select(.id == "device_platform" and .status == "unknown")] | length == 1' "$first" >/dev/null; then
  echo "stale empty catalog must not report a definite current version" >&2
  exit 1
fi

printf '%s\n' 'worker offline recovery test passed.'
