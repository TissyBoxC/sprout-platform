#!/usr/bin/env bash
# Consume brand-service upgrade requests from the shared durable volume.
#
# The worker has no HTTP listener. Only the admin backend may create queue
# files; this process validates each request, serializes execution, and writes
# a pollable result. Mounting docker.sock is equivalent to host root access, so
# this script never evaluates input or forwards raw request fields to a shell.
set -euo pipefail

root_dir="${SPROUT_SERVICE_VERSION_STATE_DIR:-/var/lib/sprout-upgrades}"
queue_dir="$root_dir/queue"
processing_dir="$root_dir/processing"
results_dir="$root_dir/results"
cloud_dir="${SPROUT_SERVICE_VERSION_CLOUD_DIR:-/opt/sprout-upgrade}"
host_cloud_dir="${SPROUT_SERVICE_VERSION_HOST_CLOUD_DIR:-$cloud_dir}"
upgrade_script="$cloud_dir/scripts/upgrade-service.sh"

status_interval="${SPROUT_SERVICE_VERSION_CHECK_INTERVAL_SECONDS:-60}"
poll_interval="${SPROUT_SERVICE_VERSION_POLL_INTERVAL_SECONDS:-5}"
max_request_bytes="${SPROUT_SERVICE_VERSION_MAX_REQUEST_BYTES:-65536}"
state_uid="${SPROUT_SERVICE_VERSION_STATE_UID:-65532}"
state_gid="${SPROUT_SERVICE_VERSION_STATE_GID:-65532}"
release_cache_dir="$root_dir/.release-cache"
release_cache_ttl="${SPROUT_SERVICE_VERSION_RELEASE_CACHE_SECONDS:-300}"
release_catalog_limit="${SPROUT_SERVICE_VERSION_RELEASE_CATALOG_LIMIT:-100}"
release_catalog_retry_interval="${SPROUT_SERVICE_VERSION_RELEASE_CATALOG_RETRY_SECONDS:-30}"
release_catalog_state_file="$root_dir/.releases-state"
release_catalog_next_refresh_at=0
platform_repository="${SPROUT_UPGRADE_PLATFORM_REPOSITORY:-TissyBoxC/sprout-platform}"
sub2api_repository="${SPROUT_UPGRADE_SUB2API_REPOSITORY:-TissyBoxC/sprout-sub2api-fork}"

service_ids="device_platform voice_gateway admin_web sub2api postgres redis mqtt download_init download_ftp download_http"
upgrade_order="sub2api device_platform voice_gateway admin_web"
active_operation_service=""
active_operation_status=""

service_display_name() {
  service="$1"
  case "$service" in
    device_platform)
      printf '%s\n' "设备平台"
      ;;
    voice_gateway)
      printf '%s\n' "语音网关"
      ;;
    admin_web)
      printf '%s\n' "品牌管理端"
      ;;
    sub2api)
      printf '%s\n' "品牌 AI 网关"
      ;;
  esac
}

service_role() {
  service="$1"
  case "$service" in
    device_platform|voice_gateway|admin_web)
      printf '%s\n' "platform"
      ;;
    sub2api)
      printf '%s\n' "ai_gateway"
      ;;
    postgres|redis|mqtt)
      printf '%s\n' "infrastructure"
      ;;
    download_init|download_ftp|download_http)
      printf '%s\n' "download"
      ;;
  esac
}

service_image() {
  service="$1"
  case "$service" in
    device_platform)
      printf '%s\n' "ghcr.io/tissyboxc/sprout-device-platform"
      ;;
    voice_gateway)
      printf '%s\n' "ghcr.io/tissyboxc/sprout-voice-gateway"
      ;;
    admin_web)
      printf '%s\n' "ghcr.io/tissyboxc/sprout-admin-web"
      ;;
    sub2api)
      printf '%s\n' "ghcr.io/tissyboxc/sub2api"
      ;;
    postgres)
      printf '%s\n' "postgres"
      ;;
    redis)
      printf '%s\n' "redis"
      ;;
    mqtt)
      printf '%s\n' "eclipse-mosquitto"
      ;;
    download_init)
      printf '%s\n' "alpine"
      ;;
    download_ftp)
      printf '%s\n' "atmoz/sftp"
      ;;
    download_http)
      printf '%s\n' "nginx"
      ;;
  esac
}

service_display_name_fallback() {
  service="$1"
  case "$service" in
    postgres)
      printf '%s\n' "PostgreSQL 数据库"
      ;;
    redis)
      printf '%s\n' "Redis 缓存"
      ;;
    mqtt)
      printf '%s\n' "MQTT 消息服务"
      ;;
    download_init)
      printf '%s\n' "下载目录初始化"
      ;;
    download_ftp)
      printf '%s\n' "发布文件上传"
      ;;
    download_http)
      printf '%s\n' "发布文件下载"
      ;;
    *)
      printf '%s\n' "$service"
      ;;
  esac
}

service_repository() {
  service="$1"
  case "$service" in
    sub2api)
      printf '%s\n' "TissyBoxC/sprout-sub2api-fork"
      ;;
    *)
      printf '%s\n' "TissyBoxC/sprout-platform"
      ;;
  esac
}

is_managed_service() {
  case "$1" in
    device_platform|voice_gateway|admin_web|sub2api)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

validate_positive_integer() {
  name="$1"
  value="$2"
  minimum="$3"
  maximum="$4"
  if ! printf '%s' "$value" | grep -Eq '^[0-9]+$' ||
    [ "$value" -lt "$minimum" ] ||
    [ "$value" -gt "$maximum" ]; then
    echo "$name 必须是 ${minimum} 到 ${maximum} 之间的整数。" >&2
    exit 1
  fi
}

# The worker runs from the host-mounted checkout and must not depend on the
# executor script having been sourced in the same process.
require_command() {
  command_name="$1"
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "缺少命令: $command_name" >&2
    exit 1
  fi
}

validate_operation_id() {
  operation_id="$1"
  printf '%s' "$operation_id" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$'
}

validate_service() {
  service="$1"
  case "$service" in
    device_platform|voice_gateway|admin_web|sub2api)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

is_known_service() {
  case " $service_ids " in
    *" $1 "*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

iso_timestamp() {
  date -u +%Y-%m-%dT%H:%M:%SZ
}

write_json_atomic() {
  destination="$1"
  temp_file="$(mktemp "$(dirname -- "$destination")/.json.XXXXXX")"
  cat > "$temp_file"
  chmod 640 "$temp_file"
  chown "$state_uid:$state_gid" "$temp_file" 2>/dev/null || true
  mv "$temp_file" "$destination"
}

set_active_operation() {
  active_operation_service="$1"
  active_operation_status="$2"
}

clear_active_operation() {
  active_operation_service=""
  active_operation_status=""
}

write_result_snapshot() {
  operation_id="$1"
  service="$2"
  current_version="$3"
  target_version="$4"
  status="$5"
  message="$6"
  started_at="$7"
  finished_at="$8"
  log_file="$9"

  if [ -f "$log_file" ]; then
    log_tail="$(tail -c 4096 "$log_file" 2>/dev/null || true)"
    # The worker may run commands inherited from the compose environment.
    # Redact values that are already known secrets before operators read them.
    for secret_name in \
      SPROUT_POSTGRES_PASSWORD \
      SPROUT_REDIS_PASSWORD \
      SPROUT_INTERNAL_SERVICE_TOKEN \
      SPROUT_AUTH_ACCESS_TOKEN_SECRET \
      SPROUT_AI_CREDENTIAL_KEY \
      SPROUT_MFA_CREDENTIAL_KEY \
      SPROUT_SUB2API_JWT_SECRET \
      SPROUT_SUB2API_TOTP_ENCRYPTION_KEY \
      SPROUT_SUB2API_API_KEY \
      GITHUB_TOKEN; do
      secret_value="${!secret_name:-}"
      if [ -n "$secret_value" ]; then
        log_tail="${log_tail//"$secret_value"/[已隐藏]}"
      fi
    done
  else
    log_tail=""
  fi

  # The queue file is deleted after the operation, so carry the requester into
  # the result or the audit trail loses who started the upgrade.
  requested_by=""
  requested_at=""
  if [ -n "${processing_file:-}" ] && [ -f "$processing_file" ]; then
    requested_by="$(jq -r '.requested_by // empty' "$processing_file" 2>/dev/null || true)"
    requested_at="$(jq -r '.requested_at // empty' "$processing_file" 2>/dev/null || true)"
  fi

  jq -n \
    --arg id "$operation_id" \
    --arg target_service "$service" \
    --arg current_version "$current_version" \
    --arg target_version "$target_version" \
    --arg status "$status" \
    --arg message "$message" \
    --arg started_at "$started_at" \
    --argjson finished_at "$finished_at" \
    --arg log_tail "$log_tail" \
    --arg requested_by "$requested_by" \
    --argjson requested_at "$(json_string_or_null "$requested_at")" \
    '{
      id: $id,
      target_service: $target_service,
      current_version: $current_version,
      target_version: $target_version,
      status: $status,
      message: $message,
      started_at: $started_at,
      finished_at: $finished_at,
      log_tail: $log_tail,
      requested_by: $requested_by,
      requested_at: $requested_at
    }' |
    write_json_atomic "$results_dir/${operation_id}.json"
}

json_string_or_null() {
  value="$1"
  if [ -n "$value" ]; then
    jq -n --arg value "$value" '$value'
  else
    printf '%s\n' 'null'
  fi
}

read_running_image_tag() {
  service="$1"
  compose_service="$service"
  container_id="$(
    docker compose \
      --project-directory "$host_cloud_dir" \
      -f "$cloud_dir/docker-compose.yml" \
      --env-file "$cloud_dir/.env" \
      ps -q "$compose_service" 2>/dev/null || true
  )"
  if [ -z "$container_id" ]; then
    printf '%s\n' "unknown"
    return 0
  fi

  image_tag="$(
    docker inspect --format '{{.Config.Image}}' "$container_id" 2>/dev/null |
      awk -F: 'NF > 1 { print $NF }' || true
  )"
  if [ -n "$image_tag" ]; then
    printf '%s\n' "$image_tag"
  else
    printf '%s\n' "unknown"
  fi
}

read_running_image_version() {
  image_tag="$(read_running_image_tag "$1")"
  if validate_version "$image_tag"; then
    printf '%s\n' "$image_tag"
  else
    printf '%s\n' "unknown"
  fi
}

validate_version() {
  version="$1"
  printf '%s' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'
}

version_is_older() {
  current_version="$1"
  latest_version="$2"
  awk -v current_version="$current_version" -v latest_version="$latest_version" '
    BEGIN {
      split(current_version, current_parts, ".")
      split(latest_version, latest_parts, ".")
      for (part = 1; part <= 3; part++) {
        if ((current_parts[part] + 0) < (latest_parts[part] + 0)) {
          exit 0
        }
        if ((current_parts[part] + 0) > (latest_parts[part] + 0)) {
          exit 1
        }
      }
      exit 1
    }
  '
}

resolve_latest_release() {
  repository="$1"
  force_refresh="${2:-0}"
  cache_key="$(printf '%s' "$repository" | tr '/.' '__')"
  cache_file="$release_cache_dir/${cache_key}.json"
  cache_stamp="$release_cache_dir/${cache_key}.stamp"
  cache_now="$(date +%s)"

  if [ "$force_refresh" != "1" ] &&
    [ -s "$cache_file" ] &&
    [ -f "$cache_stamp" ]; then
    cache_age="$((cache_now - $(cat "$cache_stamp" 2>/dev/null || printf '%s' 0)))"
    if [ "$cache_age" -ge 0 ] && [ "$cache_age" -lt "$release_cache_ttl" ]; then
      cached_version="$(jq -r '.version // empty' "$cache_file" 2>/dev/null || true)"
      cached_url="$(jq -r '.release_url // empty' "$cache_file" 2>/dev/null || true)"
      if validate_version "$cached_version"; then
        resolved_release_version="$cached_version"
        resolved_release_url="$cached_url"
        return 0
      fi
    fi
  fi

  response_file="$(mktemp)"
  curl_args=(
    --fail
    --silent
    --show-error
    --location
    --max-time 20
    --header "Accept: application/vnd.github+json"
    --header "User-Agent: sprout-service-upgrade-worker"
  )
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    curl_args+=(--header "Authorization: Bearer ${GITHUB_TOKEN}")
  fi

  if ! curl "${curl_args[@]}" \
    "https://api.github.com/repos/${repository}/releases/latest" \
    --output "$response_file"; then
    rm -f "$response_file"
    return 1
  fi

  release_tag="$(jq -r '.tag_name // empty' "$response_file" 2>/dev/null || true)"
  release_url="$(jq -r '.html_url // empty' "$response_file" 2>/dev/null || true)"
  rm -f "$response_file"
  resolved_release_version="${release_tag#v}"
  if ! validate_version "$resolved_release_version"; then
    resolved_release_version=""
    resolved_release_url=""
    return 1
  fi
  resolved_release_url="$release_url"
  jq -n \
    --arg version "$resolved_release_version" \
    --arg release_url "$resolved_release_url" \
    '{version: $version, release_url: $release_url}' > "$cache_file"
  date +%s > "$cache_stamp"
}

list_releases_for_repository() {
  repository="$1"
  per_page="$2"
  response_file="$(mktemp)"
  curl_args=(
    --fail
    --silent
    --show-error
    --location
    --max-time 20
    --header "Accept: application/vnd.github+json"
    --header "User-Agent: sprout-service-upgrade-worker"
  )
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    curl_args+=(--header "Authorization: Bearer ${GITHUB_TOKEN}")
  fi

  if ! curl "${curl_args[@]}" \
    "https://api.github.com/repos/${repository}/releases?per_page=${per_page}&page=1" \
    --output "$response_file"; then
    rm -f "$response_file"
    return 1
  fi

  jq -c '
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
  ' "$response_file"
  rm -f "$response_file"
}

write_release_catalog() {
  generated_at="$(iso_timestamp)"
  platform_releases="$(list_releases_for_repository "$platform_repository" "$release_catalog_limit")" || return 1
  sub2api_releases="$(list_releases_for_repository "$sub2api_repository" "$release_catalog_limit")" || return 1

  catalog_temp="$(mktemp "$root_dir/.releases.json.XXXXXX")"
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
    }' > "$catalog_temp"
  chmod 640 "$catalog_temp"
  chown "$state_uid:$state_gid" "$catalog_temp" 2>/dev/null || true
  mv "$catalog_temp" "$root_dir/releases.json"
  release_catalog_next_refresh_at=$(( $(date +%s) + release_cache_ttl ))
  printf '%s\n' "$release_catalog_next_refresh_at" > "$release_catalog_state_file"
  chmod 640 "$release_catalog_state_file"
  chown "$state_uid:$state_gid" "$release_catalog_state_file" 2>/dev/null || true
}

release_catalog_is_valid() {
  [ -s "$root_dir/releases.json" ] &&
    jq -e '
      type == "object"
      and (.generated_at | type == "string" and length > 0)
      and (.services | type == "object")
      and (.services.device_platform | type == "array")
      and (.services.voice_gateway | type == "array")
      and (.services.admin_web | type == "array")
      and (.services.sub2api | type == "array")
    ' "$root_dir/releases.json" >/dev/null 2>&1
}

refresh_release_catalog() {
  force_refresh="${1:-0}"
  now="$(date +%s)"
  catalog_exists=0
  if [ -f "$root_dir/releases.json" ]; then
    catalog_exists=1
  fi
  if [ -f "$release_catalog_state_file" ]; then
    persisted_next_refresh_at="$(cat "$release_catalog_state_file" 2>/dev/null || true)"
    if printf '%s' "$persisted_next_refresh_at" | grep -Eq '^[0-9]+$'; then
      release_catalog_next_refresh_at="$persisted_next_refresh_at"
    fi
  fi

  if [ "$force_refresh" != "1" ] &&
    [ "$catalog_exists" -eq 1 ] &&
    [ "$now" -lt "$release_catalog_next_refresh_at" ] &&
    release_catalog_is_valid; then
    return 0
  fi

  if [ "$force_refresh" = "1" ] ||
    [ "$catalog_exists" -eq 0 ] ||
    ! release_catalog_is_valid ||
    [ "$now" -ge "$release_catalog_next_refresh_at" ]; then
    if write_release_catalog; then
      return 0
    fi
    release_catalog_next_refresh_at=$((now + release_catalog_retry_interval))
    printf '%s\n' "$release_catalog_next_refresh_at" > "$release_catalog_state_file"
    chmod 640 "$release_catalog_state_file"
    chown "$state_uid:$state_gid" "$release_catalog_state_file" 2>/dev/null || true
    return 1
  fi

  return 0
}

write_status_snapshot() {
  force_refresh="${1:-0}"
  status_temp="$(mktemp "$root_dir/.status.XXXXXX")"
  services_file="$(mktemp)"
  generated_at="$(iso_timestamp)"

  for service in $service_ids; do
    current_version="$(read_running_image_tag "$service")"
    latest_version=""
    release_url=""
    status="unknown"
    checked_at="$generated_at"
    display_name="$(service_display_name "$service")"
    if [ -z "$display_name" ]; then
      display_name="$(service_display_name_fallback "$service")"
    fi

    if is_managed_service "$service" && resolve_latest_release "$(service_repository "$service")" "$force_refresh"; then
      latest_version="$resolved_release_version"
      release_url="$resolved_release_url"
      if [ "$current_version" = "unknown" ] || ! validate_version "$current_version"; then
        status="unknown"
      elif [ "$current_version" = "$latest_version" ] ||
        ! version_is_older "$current_version" "$latest_version"; then
        status="current"
      else
        status="outdated"
      fi
    fi

    # The console polls the same snapshot while an operation is running. Keep
    # the active service visibly busy instead of reporting a stale "outdated"
    # value between queue pickup and the next periodic refresh.
    if [ "$active_operation_service" = "$service" ]; then
      case "$active_operation_status" in
        queued|running|recovering)
          status="updating"
          ;;
        failed)
          status="failed"
          ;;
      esac
    fi

    releases_json="$(jq -c --arg id "$service" \
      '.services[$id] // []' "$root_dir/releases.json" 2>/dev/null || printf '%s' '[]')"
    jq -n \
      --arg id "$service" \
      --arg display_name "$display_name" \
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
        # Older backend readers used last_checked_at; keep both names while
        # the file contract and console model are being aligned.
        last_checked_at: $checked_at
      }' >> "$services_file"
  done

  jq -s \
    --arg generated_at "$generated_at" \
    '{generated_at: $generated_at, services: .}' \
    "$services_file" > "$status_temp"
  rm -f "$services_file"
  chmod 640 "$status_temp"
  chown "$state_uid:$state_gid" "$status_temp" 2>/dev/null || true
  mv "$status_temp" "$root_dir/status.json"
}

consume_check_request() {
  request_file="$root_dir/check-request.json"
  if [ ! -f "$request_file" ]; then
    return 1
  fi

  # Refresh first. A malformed request is removed afterwards so it can never
  # block future checks, but the admin still receives a fresh snapshot.
  if ! refresh_release_catalog 1; then
    rm -f "$request_file"
    return 1
  fi
  write_status_snapshot 1
  rm -f "$request_file"
  return 0
}

next_operation_file() {
  # Queue files are named by operation id, so lexical order is random. Read
  # the validated service field to preserve the fixed dependency order.
  find "$queue_dir" -maxdepth 1 -type f -name '*.json' -print 2>/dev/null |
    while IFS= read -r candidate_file; do
      candidate_service="$(
        jq -r '.target_service // empty' "$candidate_file" 2>/dev/null ||
          true
      )"
      candidate_rank="$(
        case "$candidate_service" in
          sub2api) printf '%s\n' 10 ;;
          device_platform) printf '%s\n' 20 ;;
          voice_gateway) printf '%s\n' 30 ;;
          admin_web) printf '%s\n' 40 ;;
          *) printf '%s\n' 90 ;;
        esac
      )"
      candidate_requested_at="$(
        jq -r '.requested_at // empty' "$candidate_file" 2>/dev/null ||
          true
      )"
      printf '%s\t%s\t%s\n' \
        "$candidate_rank" \
        "$candidate_requested_at" \
        "$candidate_file"
    done |
    sort -k1,1n -k2,2 |
    head -n 1 |
    cut -f3-
}

process_operation() {
  operation_file="$1"
  basename_value="$(basename -- "$operation_file")"
  operation_id="${basename_value%.json}"
  if ! validate_operation_id "$operation_id"; then
    rm -f "$operation_file"
    return 0
  fi

  processing_file="$processing_dir/${operation_id}.json"
  # mv is the ownership claim. If another worker won the race, it already
  # moved the queue file and this path no longer exists.
  if ! mv -n "$operation_file" "$processing_file" 2>/dev/null; then
    return 0
  fi

  request_size="$(wc -c < "$processing_file" | tr -d ' ')"
  if [ "$request_size" -gt "$max_request_bytes" ]; then
    write_result_snapshot "$operation_id" "unknown" "unknown" "" "failed" "升级请求超过允许大小。" "$(iso_timestamp)" "$(json_string_or_null "$(iso_timestamp)")" /dev/null
    rm -f "$processing_file"
    return 0
  fi

  if ! jq -e \
    --arg id "$operation_id" \
    'type == "object" and .id == $id and (.target_service | type == "string") and (.target_version | type == "string") and (.requested_at | type == "string") and (.requested_by | type == "string")' \
    "$processing_file" >/dev/null 2>&1; then
    write_result_snapshot "$operation_id" "unknown" "unknown" "" "failed" "升级请求格式不正确。" "$(iso_timestamp)" "$(json_string_or_null "$(iso_timestamp)")" /dev/null
    rm -f "$processing_file"
    return 0
  fi

  service="$(jq -r '.target_service' "$processing_file")"
  target_version="$(jq -r '.target_version' "$processing_file")"
  if ! is_known_service "$service"; then
    write_result_snapshot "$operation_id" "$service" "unknown" "$target_version" "failed" "没有找到这个服务。" "$(iso_timestamp)" "$(json_string_or_null "$(iso_timestamp)")" /dev/null
    rm -f "$processing_file"
    return 0
  fi
  if ! validate_version "$target_version"; then
    write_result_snapshot "$operation_id" "$service" "unknown" "$target_version" "failed" "目标版本格式不正确。" "$(iso_timestamp)" "$(json_string_or_null "$(iso_timestamp)")" /dev/null
    rm -f "$processing_file"
    return 0
  fi
  if ! validate_service "$service"; then
    write_result_snapshot "$operation_id" "$service" "unknown" "$target_version" "failed" "该服务由部署配置统一维护，不能通过版本管理升级。" "$(iso_timestamp)" "$(json_string_or_null "$(iso_timestamp)")" /dev/null
    rm -f "$processing_file"
    return 0
  fi

  started_at="$(iso_timestamp)"
  current_version="$(read_running_image_version "$service")"
  log_file="$(mktemp)"

  # The result contract supports queued, so publish it before the first slow
  # Docker operation. The admin backend may poll immediately after enqueue.
  write_result_snapshot "$operation_id" "$service" "$current_version" "$target_version" "queued" "升级任务已开始处理。" "$started_at" "null" "$log_file"
  set_active_operation "$service" "running"
  write_status_snapshot

  result_message="正在升级 ${service} 到 ${target_version}。"
  write_result_snapshot "$operation_id" "$service" "$current_version" "$target_version" "running" "$result_message" "$started_at" "null" "$log_file"

  set +e
  bash "$upgrade_script" "$service" "$target_version" >"$log_file" 2>&1
  exit_code=$?
  set -e

  if [ "$exit_code" -eq 0 ]; then
    finished_at="$(iso_timestamp)"
    if [ "$service" = "admin_web" ]; then
      result_message="管理端已重启，稍后自动恢复。"
    else
      result_message="服务已升级到 ${target_version}。"
    fi
    write_result_snapshot "$operation_id" "$service" "$current_version" "$target_version" "succeeded" "$result_message" "$started_at" "$(json_string_or_null "$finished_at")" "$log_file"
  else
    finished_at="$(iso_timestamp)"
    result_message="升级失败，已尝试恢复旧版本。请查看升级日志。"
    write_result_snapshot "$operation_id" "$service" "$current_version" "$target_version" "failed" "$result_message" "$started_at" "$(json_string_or_null "$finished_at")" "$log_file"
  fi

  if [ "$exit_code" -eq 0 ]; then
    clear_active_operation
  else
    set_active_operation "$service" "failed"
  fi
  write_status_snapshot
  # A failed operation is visible for one snapshot, then the next periodic
  # refresh returns to the actual running version instead of staying failed.
  clear_active_operation
  rm -f "$processing_file"
  rm -f "$log_file"
}

main() {
  validate_positive_integer "SPROUT_SERVICE_VERSION_CHECK_INTERVAL_SECONDS" "$status_interval" 10 86400
  validate_positive_integer "SPROUT_SERVICE_VERSION_POLL_INTERVAL_SECONDS" "$poll_interval" 1 3600
  validate_positive_integer "SPROUT_SERVICE_VERSION_MAX_REQUEST_BYTES" "$max_request_bytes" 1024 1048576
  validate_positive_integer "SPROUT_SERVICE_VERSION_RELEASE_CACHE_SECONDS" "$release_cache_ttl" 0 86400
  validate_positive_integer "SPROUT_SERVICE_VERSION_RELEASE_CATALOG_LIMIT" "$release_catalog_limit" 1 500
  validate_positive_integer "SPROUT_SERVICE_VERSION_RELEASE_CATALOG_RETRY_SECONDS" "$release_catalog_retry_interval" 5 3600
  validate_positive_integer "SPROUT_SERVICE_VERSION_STATE_UID" "$state_uid" 0 4294967295
  validate_positive_integer "SPROUT_SERVICE_VERSION_STATE_GID" "$state_gid" 0 4294967295

  mkdir -p "$queue_dir" "$processing_dir" "$results_dir" "$release_cache_dir"
  chown -R "$state_uid:$state_gid" "$root_dir" 2>/dev/null || true

  if [ ! -f "$upgrade_script" ]; then
    echo "升级执行器不可用: $upgrade_script" >&2
    exit 1
  fi

  require_command docker
  require_command jq
  require_command curl

  if ! refresh_release_catalog 1; then
    if [ -f "$root_dir/releases.json" ]; then
      echo "发布版本列表刷新失败，继续使用上一次成功生成的版本目录。" >&2
    else
      echo "发布版本列表初始化失败，worker 将在缓存过期后重试。" >&2
    fi
  fi

  next_status_at=0
  while true; do
    now="$(date +%s)"
    if consume_check_request; then
      :
    elif [ "$now" -ge "$next_status_at" ]; then
      refresh_release_catalog || true
      write_status_snapshot
      next_status_at=$((now + status_interval))
    fi

    operation_file="$(next_operation_file)"
    if [ -n "$operation_file" ]; then
      process_operation "$operation_file"
      continue
    fi

    sleep "$poll_interval"
  done
}

main "$@"
