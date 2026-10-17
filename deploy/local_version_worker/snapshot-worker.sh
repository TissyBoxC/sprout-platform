#!/usr/bin/env bash
# Publish read-only service version metadata for the local console.
#
# This process deliberately has no Docker socket and writes only the two
# snapshots the console reads. Upgrade execution remains unavailable locally.
set -euo pipefail

root_dir="${SPROUT_LOCAL_VERSION_STATE_DIR:-/var/lib/sprout-upgrades}"
interval="${SPROUT_LOCAL_VERSION_CHECK_INTERVAL_SECONDS:-300}"
request_file="$root_dir/check-request.json"
platform_repository="${SPROUT_LOCAL_VERSION_PLATFORM_REPOSITORY:-TissyBoxC/sprout-platform}"
sub2api_repository="${SPROUT_LOCAL_VERSION_SUB2API_REPOSITORY:-TissyBoxC/sprout-sub2api-fork}"

platform_version="${SPROUT_PLATFORM_VERSION:-unknown}"
sub2api_version="${SPROUT_SUB2API_VERSION:-unknown}"
service_ids="device_platform voice_gateway admin_web sub2api postgres redis mqtt download_init download_ftp download_http"

if ! printf '%s' "$interval" | grep -Eq '^[0-9]+$' || [ "$interval" -lt 30 ]; then
  echo "SPROUT_LOCAL_VERSION_CHECK_INTERVAL_SECONDS must be an integer of at least 30." >&2
  exit 1
fi

for command_name in curl jq date mktemp; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "Missing required command: $command_name" >&2
    exit 1
  fi
done

iso_timestamp() {
  date -u +%Y-%m-%dT%H:%M:%SZ
}

validate_version() {
  printf '%s' "$1" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'
}

version_is_older() {
  awk -v older_version="$1" -v newer_version="$2" '
    BEGIN {
      split(older_version, older_parts, ".")
      split(newer_version, newer_parts, ".")
      for (part = 1; part <= 3; part++) {
        if ((older_parts[part] + 0) < (newer_parts[part] + 0)) {
          exit 0
        }
        if ((older_parts[part] + 0) > (newer_parts[part] + 0)) {
          exit 1
        }
      }
      exit 1
    }
  '
}

write_json_atomic() {
  destination="$1"
  temp_file="$(mktemp "$(dirname -- "$destination")/.json.XXXXXX")"
  cat > "$temp_file"
  chmod 0640 "$temp_file"
  mv "$temp_file" "$destination"
}

service_display_name() {
  case "$1" in
    device_platform) printf '%s\n' "设备平台" ;;
    voice_gateway) printf '%s\n' "语音网关" ;;
    admin_web) printf '%s\n' "品牌管理端" ;;
    sub2api) printf '%s\n' "品牌 AI 网关" ;;
    postgres) printf '%s\n' "PostgreSQL 数据库" ;;
    redis) printf '%s\n' "Redis 缓存" ;;
    mqtt) printf '%s\n' "MQTT 消息服务" ;;
    download_init) printf '%s\n' "下载目录初始化" ;;
    download_ftp) printf '%s\n' "发布文件上传" ;;
    download_http) printf '%s\n' "发布文件下载" ;;
  esac
}

service_role() {
  case "$1" in
    device_platform|voice_gateway|admin_web) printf '%s\n' "platform" ;;
    sub2api) printf '%s\n' "ai_gateway" ;;
    postgres|redis|mqtt) printf '%s\n' "infrastructure" ;;
    download_init|download_ftp|download_http) printf '%s\n' "download" ;;
  esac
}

service_image() {
  case "$1" in
    device_platform) printf '%s\n' "sprout-local-device_platform" ;;
    voice_gateway) printf '%s\n' "sprout-local-voice_gateway" ;;
    admin_web) printf '%s\n' "sprout-local-admin_web" ;;
    sub2api) printf '%s\n' "sprout-local-sub2api" ;;
    postgres) printf '%s\n' "postgres" ;;
    redis) printf '%s\n' "redis" ;;
    mqtt) printf '%s\n' "eclipse-mosquitto" ;;
    download_init) printf '%s\n' "alpine" ;;
    download_ftp) printf '%s\n' "atmoz/sftp" ;;
    download_http) printf '%s\n' "nginx" ;;
  esac
}

service_version() {
  case "$1" in
    device_platform|voice_gateway|admin_web) printf '%s\n' "$platform_version" ;;
    sub2api) printf '%s\n' "$sub2api_version" ;;
    *) printf '%s\n' "unknown" ;;
  esac
}

is_managed_service() {
  case "$1" in
    device_platform|voice_gateway|admin_web|sub2api) return 0 ;;
    *) return 1 ;;
  esac
}

list_releases() {
  repository="$1"
  response_file="$(mktemp)"
  if ! curl \
    --fail \
    --silent \
    --show-error \
    --location \
    --max-time 20 \
    --header "Accept: application/vnd.github+json" \
    --header "User-Agent: sprout-local-version-worker" \
    "https://api.github.com/repos/${repository}/releases?per_page=100&page=1" \
    --output "$response_file"; then
    rm -f "$response_file"
    return 1
  fi
  if ! jq -c '
    [
      .[]
      | select(.draft == false and .prerelease == false)
      | {
          version: (.tag_name | sub("^v"; "")),
          release_url: (.html_url // ""),
          published_at: (.published_at // null),
          is_current: false,
          is_latest: false
        }
      | select(.version | test("^[0-9]+\\.[0-9]+\\.[0-9]+$"))
    ]
  ' "$response_file"; then
    rm -f "$response_file"
    return 1
  fi
  rm -f "$response_file"
}

write_snapshots() {
  generated_at="$(iso_timestamp)"
  platform_releases="$(list_releases "$platform_repository")" || return 1
  sub2api_releases="$(list_releases "$sub2api_repository")" || return 1

  jq -n \
    --arg generated_at "$generated_at" \
    --argjson platform_releases "$platform_releases" \
    --argjson sub2api_releases "$sub2api_releases" \
    '{
      generated_at: $generated_at,
      services: {
        device_platform: $platform_releases,
        voice_gateway: $platform_releases,
        admin_web: $platform_releases,
        sub2api: $sub2api_releases
      }
    }' | write_json_atomic "$root_dir/releases.json"

  services_file="$(mktemp)"
  for service in $service_ids; do
    current_version="$(service_version "$service")"
    latest_version=""
    release_url=""
    status="unknown"
    checked_at="$generated_at"

    if is_managed_service "$service"; then
      if [ "$service" = "sub2api" ]; then
        service_releases="$sub2api_releases"
      else
        service_releases="$platform_releases"
      fi
      latest_version="$(
        printf '%s\n' "$service_releases" | jq -r '
          map(select(.version | test("^[0-9]+\\.[0-9]+\\.[0-9]+$")))
          | sort_by(.version | split(".") | map(tonumber))
          | reverse
          | .[0].version // empty
        '
      )"
      release_url="$(
        printf '%s\n' "$service_releases" | jq -r --arg version "$latest_version" '
          map(select(.version == $version)) | .[0].release_url // empty
        '
      )"
      if validate_version "$current_version" && validate_version "$latest_version"; then
        if [ "$current_version" = "$latest_version" ] ||
          ! version_is_older "$current_version" "$latest_version"; then
          status="current"
        else
          status="outdated"
        fi
      fi
    fi

    releases_json="$(
      printf '%s\n' "$service_releases" | jq -c \
        --arg current_version "$current_version" \
        --arg latest_version "$latest_version" \
        'if . == null then [] else
          map(. + {
            is_current: ($current_version != "" and .version == $current_version),
            is_latest: ($latest_version != "" and .version == $latest_version)
          }) | sort_by(.version | split(".") | map(tonumber)) | reverse
        end'
    )"
    if ! is_managed_service "$service"; then
      releases_json="[]"
    fi

    jq -n \
      --arg id "$service" \
      --arg display_name "$(service_display_name "$service")" \
      --arg role "$(service_role "$service")" \
      --arg image "$(service_image "$service")" \
      --arg current_version "$current_version" \
      --arg latest_version "$latest_version" \
      --arg status "$status" \
      --arg release_url "$release_url" \
      --arg checked_at "$checked_at" \
      --argjson releases "$releases_json" \
      '{
        id: $id,
        display_name: $display_name,
        role: $role,
        image: $image,
        current_version: $current_version,
        latest_version: $latest_version,
        status: $status,
        release_url: $release_url,
        releases: $releases,
        checked_at: $checked_at,
        last_checked_at: $checked_at
      }' >> "$services_file"
  done

  jq -s \
    --arg generated_at "$generated_at" \
    '{generated_at: $generated_at, services: .}' \
    "$services_file" | write_json_atomic "$root_dir/status.json"
  rm -f "$services_file"
}

mkdir -p "$root_dir"
next_refresh_at=0

while true; do
  now="$(date +%s)"
  force_refresh=0
  if [ -f "$request_file" ]; then
    rm -f "$request_file"
    force_refresh=1
  fi
  if [ "$force_refresh" -eq 1 ] ||
    [ "$now" -ge "$next_refresh_at" ] ||
    [ ! -s "$root_dir/status.json" ] ||
    [ ! -s "$root_dir/releases.json" ]; then
    if write_snapshots; then
      next_refresh_at=$((now + interval))
    else
      echo "Unable to refresh local service version snapshots; retaining the last successful snapshot." >&2
      next_refresh_at=$((now + 30))
    fi
  fi
  sleep 5
done
