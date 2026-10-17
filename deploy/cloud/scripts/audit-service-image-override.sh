#!/usr/bin/env bash
# Audit a production-only managed image override without changing the stack.
#
# The upgrade executor refuses to upgrade a managed service while
# docker-compose.override.yml pins its image. Operators still need a safe way
# to tell whether the local image is a required patch or merely a stale copy of
# the published image. This script compares the running stack, the override,
# the locally built image, and the same registry tag.
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
default_cloud_dir="/opt/1panel/apps/sprout/deploy/cloud"
relative_cloud_dir="$(CDPATH= cd -- "$script_dir/.." 2>/dev/null && pwd || true)"
if [ -n "${SPROUT_UPGRADE_CLOUD_DIR:-}" ]; then
  cloud_dir="$SPROUT_UPGRADE_CLOUD_DIR"
elif [ -n "$relative_cloud_dir" ] && [ -f "$relative_cloud_dir/.env" ]; then
  cloud_dir="$relative_cloud_dir"
elif [ -f "$default_cloud_dir/.env" ]; then
  cloud_dir="$default_cloud_dir"
else
  cloud_dir="$relative_cloud_dir"
fi
compose_file="$cloud_dir/docker-compose.yml"
override_file="$cloud_dir/docker-compose.override.yml"
env_file="$cloud_dir/.env"

usage() {
  cat >&2 <<'EOF'
用法:
  audit-service-image-override.sh [--json]
  audit-service-image-override.sh --switch-to-registry [服务]

作用:
  比较生产 docker-compose.override.yml 中的本地受管镜像与同版本 GHCR 镜像。
  默认只读，不修改文件、不停止容器。

--switch-to-registry:
  仅当本地镜像与 GHCR 镜像中的受管二进制完全一致时，备份并移除受管服务
  的 image 覆盖，然后拉取 GHCR 镜像并重建该服务。发现真实差异时拒绝执行。

受管服务:
  device_platform voice_gateway admin_web sub2api
EOF
}

require_command() {
  command_name="$1"
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "缺少命令: $command_name" >&2
    exit 1
  fi
}

if [ ! -f "$env_file" ]; then
  echo "缺少云端环境文件: $env_file" >&2
  exit 1
fi
if [ ! -f "$compose_file" ]; then
  echo "缺少 Compose 文件: $compose_file" >&2
  exit 1
fi
if [ ! -f "$override_file" ]; then
  echo "未找到 docker-compose.override.yml；生产没有本地镜像覆盖。" >&2
  exit 0
fi

require_command docker
require_command awk
require_command sha256sum

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

read_env_value() {
  key="$1"
  awk -F= -v key="$key" '
    $1 == key {
      sub(/^[^=]*=/, "")
      sub(/\r$/, "")
      print
      exit
    }
  ' "$env_file"
}

override_entries() {
  awk '
    /^[[:space:]]*[A-Za-z0-9_.-]+:[[:space:]]*$/ {
      section = $1
      sub(/:$/, "", section)
      next
    }
    /^[[:space:]]+image:[[:space:]]*/ && section != "" {
      image = $0
      sub(/^[[:space:]]*image:[[:space:]]*/, "", image)
      sub(/[[:space:]]*$/, "", image)
      print section "\t" image
    }
  ' "$override_file"
}

running_image() {
  service="$1"
  container_id="$(
    docker compose \
      --project-directory "$cloud_dir" \
      -f "$compose_file" \
      --env-file "$env_file" \
      ps -q "$service" 2>/dev/null || true
  )"
  if [ -z "$container_id" ]; then
    return 1
  fi
  docker inspect --format '{{.Config.Image}}' "$container_id" 2>/dev/null
}

registry_image() {
  service="$1"
  version="$2"
  case "$service" in
    device_platform)
      printf '%s:%s\n' "ghcr.io/tissyboxc/sprout-device-platform" "$version"
      ;;
    voice_gateway)
      printf '%s:%s\n' "ghcr.io/tissyboxc/sprout-voice-gateway" "$version"
      ;;
    admin_web)
      printf '%s:%s\n' "ghcr.io/tissyboxc/sprout-admin-web" "$version"
      ;;
    sub2api)
      printf '%s:%s\n' "ghcr.io/tissyboxc/sub2api" "$version"
      ;;
  esac
}

image_binary_paths() {
  case "$1" in
    device_platform)
      printf '%s\n' '/device-platform'
      printf '%s\n' '/device-platform-admin'
      ;;
    voice_gateway)
      printf '%s\n' '/voice-gateway'
      ;;
    admin_web)
      printf '%s\n' '/usr/share/nginx/html/index.html'
      ;;
    sub2api)
      printf '%s\n' '/app/sub2api'
      ;;
  esac
}

image_binary_sha256() {
  image="$1"
  binary_path="$2"
  docker run --rm --entrypoint sh "$image" -c \
    'path="$1"; if [ -f "$path" ]; then sha256sum "$path" | awk "{print \$1}"; else printf "%s\n" missing; fi' \
    sh "$binary_path" 2>/dev/null || printf '%s\n' unavailable
}

service_version() {
  service="$1"
  case "$service" in
    sub2api)
      read_env_value SPROUT_SUB2API_VERSION
      ;;
    *)
      read_env_value SPROUT_PLATFORM_VERSION
      ;;
  esac
}

service_container_name() {
  service="$1"
  container_id="$(
    docker compose \
      --project-directory "$cloud_dir" \
      -f "$compose_file" \
      --env-file "$env_file" \
      ps -q "$service" 2>/dev/null || true
  )"
  if [ -z "$container_id" ]; then
    return 1
  fi
  docker inspect --format '{{.Name}}' "$container_id" 2>/dev/null | sed 's#^/##'
}

describe_overrides() {
  override_entries | while IFS="$(printf '\t')" read -r service image; do
    is_managed_service "$service" || continue
    version="$(service_version "$service")"
    registry="$(registry_image "$service" "$version")"
    running="$(running_image "$service" || true)"
    container="$(service_container_name "$service" || true)"
    local_id="$(docker image inspect "$image" --format '{{.Id}}' 2>/dev/null || true)"
    registry_id="$(docker image inspect "$registry" --format '{{.Id}}' 2>/dev/null || true)"
    local_created="$(docker image inspect "$image" --format '{{.Created}}' 2>/dev/null || true)"
    registry_created="$(docker image inspect "$registry" --format '{{.Created}}' 2>/dev/null || true)"

    if [ -z "$local_id" ]; then
      match="missing-local-image"
    elif [ -z "$registry_id" ]; then
      match="registry-image-not-local"
    elif [ "$local_id" = "$registry_id" ]; then
      match="identical-image"
    else
      match="different-images"
    fi

    hashes_match="unknown"
    if [ "$match" = "identical-image" ]; then
      hashes_match="yes"
    elif [ -n "$local_id" ] && [ -n "$registry_id" ]; then
      hashes_match="yes"
      while IFS= read -r binary_path; do
        local_hash="$(image_binary_sha256 "$image" "$binary_path")"
        registry_hash="$(image_binary_sha256 "$registry" "$binary_path")"
        if [ "$local_hash" != "$registry_hash" ]; then
          hashes_match="no:$binary_path"
          break
        fi
      done < <(image_binary_paths "$service")
    fi

    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
      "$service" \
      "$container" \
      "$image" \
      "$running" \
      "$version" \
      "$registry" \
      "$local_id" \
      "$registry_id" \
      "$local_created" \
      "$registry_created" \
      "$match" \
      "$hashes_match"
  done
}

print_report() {
  entries="$(describe_overrides)"
  entry_count="$(printf '%s\n' "$entries" | awk 'NF { count += 1 } END { print count + 0 }')"
  if [ "$entry_count" -le 0 ]; then
    echo "docker-compose.override.yml 未覆盖受管服务镜像。"
    return 0
  fi
  printf '%s\n' "service	container	override_image	running_image	version	registry_image	local_image_id	registry_image_id	local_created	registry_created	image_match	binary_match"
  printf '%s\n' "$entries"
  echo
  if printf '%s\n' "$entries" | awk -F '\t' '$12 != "yes" { found = 1 } END { exit !found }'; then
    echo "结论: 本地镜像包含 GHCR 同版本镜像没有的二进制变更，必须保留 override，直到补丁进入正式发布版本。"
    return 1
  fi
  echo "结论: 本地镜像与 GHCR 同版本镜像二进制一致，可以安全执行 --switch-to-registry 移除 override。"
}

switch_to_registry() {
  requested_service="${1:-}"
  if [ -n "$requested_service" ] && ! is_managed_service "$requested_service"; then
    echo "不支持的服务: $requested_service" >&2
    exit 1
  fi

  entries="$(describe_overrides)"
  entry_count="$(printf '%s\n' "$entries" | awk 'NF { count += 1 } END { print count + 0 }')"
  if [ "$entry_count" -le 0 ]; then
    echo "没有需要切换的受管镜像覆盖。"
    return 0
  fi

  if [ -n "$requested_service" ]; then
    entries="$(
      printf '%s\n' "$entries" |
        awk -F '\t' -v service="$requested_service" '$1 == service'
    )"
  fi

  filtered_count="$(printf '%s\n' "$entries" | awk 'NF { count += 1 } END { print count + 0 }')"
  if [ "$filtered_count" -le 0 ]; then
    echo "没有匹配的受管镜像覆盖: ${requested_service:-all}" >&2
    exit 1
  fi

  printf '%s\n' "$entries" |
    awk -F '\t' '$12 != "yes" { print $1, $12 }' |
    while read -r service mismatch; do
      [ -n "$service" ] || continue
      echo "拒绝切换 ${service}: 本地镜像包含 GHCR 同版本镜像没有的二进制变更 (${mismatch})。" >&2
      echo "请把补丁进入正式发布版本后再切换。" >&2
      exit 1
    done

  services_to_switch="$(
    printf '%s\n' "$entries" | awk -F '\t' 'NF { print $1 }' | sort -u
  )"
  [ -n "$services_to_switch" ] || {
    echo "没有可切换的服务。" >&2
    exit 1
  }

  backup_dir="$cloud_dir/.codex-backups/$(date -u +%Y%m%d-%H%M%S)-image-override-switch"
  mkdir -p "$backup_dir"
  cp -p "$override_file" "$backup_dir/docker-compose.override.yml"

  temp_override="$(mktemp "${override_file}.tmp.XXXXXX")"
  cp -p "$override_file" "$temp_override"
  for service in $services_to_switch; do
    awk -v service="$service" '
      BEGIN { section = ""; skip = 0 }
      /^[[:space:]]*[A-Za-z0-9_.-]+:[[:space:]]*$/ {
        section = $1
        sub(/:$/, "", section)
        skip = (section == service)
      }
      skip && /^[[:space:]]+image:[[:space:]]*/ { next }
      { print }
    ' "$temp_override" > "${temp_override}.next"
    mv "${temp_override}.next" "$temp_override"
  done

  chmod --reference="$override_file" "$temp_override" 2>/dev/null || true
  mv "$temp_override" "$override_file"

  for service in $services_to_switch; do
    registry="$(printf '%s\n' "$entries" | awk -F '\t' -v service="$service" '$1 == service { print $6; exit }')"
    docker pull "$registry"
    docker compose \
      --project-directory "$cloud_dir" \
      -f "$compose_file" \
      --env-file "$env_file" \
      up -d --no-deps "$service"
  done

  echo "受管镜像覆盖已切换为发布镜像。备份: $backup_dir"
}

if [ "${1:-}" = "--switch-to-registry" ]; then
  switch_to_registry "${2:-}"
  exit 0
fi
if [ "${1:-}" = "--json" ]; then
  print_report
  exit 0
fi
if [ "$#" -gt 0 ]; then
  usage
  exit 1
fi

print_report
