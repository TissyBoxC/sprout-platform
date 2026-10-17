#!/usr/bin/env bash
# Verify that the cloud stack is healthy and reachable on loopback.
set -euo pipefail

cloud_dir="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
compose_file="$cloud_dir/docker-compose.yml"
env_file="$cloud_dir/.env"
endpoints_file="$cloud_dir/../public-endpoints.env"

cd "$cloud_dir"

if [ ! -f "$env_file" ]; then
  echo ".env is missing; copy .env.example to .env and configure it first" >&2
  exit 1
fi

if [ -f "$endpoints_file" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$endpoints_file"
  set +a
fi

"$cloud_dir/scripts/validate-cloud-env.sh" "$env_file"

docker compose -f "$compose_file" --env-file "$env_file" ps

state_dir="$(awk -F= '
  $1 == "SPROUT_SERVICE_VERSION_STATE_DIR" {
    sub(/^[^=]*=/, "")
    sub(/\r$/, "")
    print
    exit
  }
' "$env_file")"
state_dir="${state_dir:-/var/lib/sprout-upgrades}"

echo
echo "== managed image override check =="
override_file="$cloud_dir/docker-compose.override.yml"
if [ -f "$override_file" ]; then
  if ! "$cloud_dir/scripts/upgrade-service.sh" --check-managed-image-override >/dev/null; then
    exit 1
  fi
  echo "no managed image override"
else
  echo "no docker-compose.override.yml"
fi

echo
echo "== upgrade worker =="
docker compose -f "$compose_file" --env-file "$env_file" \
  ps --format '{{.Name}} {{.Status}}' upgrade_worker
docker compose -f "$compose_file" --env-file "$env_file" \
  exec -T upgrade_worker sh -c '
    set -e
    state="${SPROUT_SERVICE_VERSION_STATE_DIR:-/var/lib/sprout-upgrades}"
    [ -s "$state/status.json" ]
    [ -s "$state/releases.json" ]
    generated_epoch="$(jq -r "try (.generated_at | fromdateiso8601) catch empty" "$state/status.json")"
    case "$generated_epoch" in
      ""|*[!0-9]*) echo "status.json generated_at is invalid" >&2; exit 1 ;;
    esac
    now_epoch="$(date +%s)"
    age=$((now_epoch - generated_epoch))
    [ "$age" -ge 0 ]
    [ "$age" -le 300 ]
    jq -e "[.services[] | select(.status == \"updating\" or .status == \"failed\")] | length == 0" "$state/status.json" >/dev/null
    test -r "$state" -a -w "$state" -a -x "$state"
    test -r "$state/queue" -a -w "$state/queue" -a -x "$state/queue"
    test -r "$state/results" -a -w "$state/results" -a -x "$state/results"
    echo "status.json fresh (${age}s), releases.json present, no active or failed upgrade"
  '

check_service() {
  name="$1"
  service="$2"
  url="$3"
  for _ in $(seq 1 60); do
    if docker compose -f "$compose_file" --env-file "$env_file" exec -T "$service" \
      wget -q -T 5 -O /dev/null "$url"; then
      echo "ready: $name ($url)"
      return 0
    fi
    sleep 3
  done
  echo "not ready: $name ($url)" >&2
  return 1
}

check_service "device platform" "device_platform" "http://127.0.0.1:8081/readyz"
check_service "voice gateway" "voice_gateway" "http://127.0.0.1:8082/readyz"
check_service "sub2api" "sub2api" "http://127.0.0.1:8080/health"
check_service "admin console" "admin_web" "http://127.0.0.1:8080/healthz"

echo
echo "== admin API proxy =="
admin_api_status="$(
  docker compose -f "$compose_file" --env-file "$env_file" exec -T admin_web \
    wget -q -S -O /dev/null http://127.0.0.1:8080/api/v1/admin/service-versions 2>&1 |
    awk '/^  HTTP\// {code=$2} END {print code}' || true
)"
if [ "$admin_api_status" != "401" ]; then
  echo "admin API proxy should return 401 without credentials, got ${admin_api_status:-no response}" >&2
  exit 1
fi
echo "admin API proxy reaches device_platform (401 unauthenticated)"

echo
echo "== device platform upgrade state access =="
docker compose -f "$compose_file" --env-file "$env_file" \
  exec -T device_platform sh -c '
    set -e
    state="${DEVICE_PLATFORM_SERVICE_VERSION_STATE_DIR:-/var/lib/sprout-upgrades}"
    test -r "$state" -a -w "$state" -a -x "$state"
    test -w "$state/queue"
    test -r "$state/results"
    echo "device platform can write the upgrade queue and read results"
  '
