#!/usr/bin/env bash
# Upgrade one managed brand service to the latest published GitHub Release.
#
# This executor is intentionally separate from upgrade-cloud.sh. The full-stack
# script owns database backups; this script only changes the version variables
# for the selected runtime service and recreates that one service.
set -euo pipefail
set -E

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
cloud_dir="$(CDPATH= cd -- "$script_dir/.." && pwd)"
compose_file="$cloud_dir/docker-compose.yml"
env_file="$cloud_dir/.env"
compose_project_dir="${SPROUT_SERVICE_VERSION_HOST_CLOUD_DIR:-${SPROUT_UPGRADE_HOST_CLOUD_DIR:-$cloud_dir}}"
platform_repository="${SPROUT_UPGRADE_PLATFORM_REPOSITORY:-TissyBoxC/sprout-platform}"
sub2api_repository="${SPROUT_UPGRADE_SUB2API_REPOSITORY:-TissyBoxC/sprout-sub2api-fork}"
github_api_base="${SPROUT_UPGRADE_GITHUB_API_BASE_URL:-https://api.github.com}"
github_token="${GITHUB_TOKEN:-${SPROUT_UPGRADE_GITHUB_TOKEN:-}}"
health_timeout="${SPROUT_UPGRADE_HEALTH_TIMEOUT_SECONDS:-180}"

upgrade_order="sub2api device_platform voice_gateway admin_web"
previous_platform_version=""
previous_sub2api_version=""
target_platform_version=""
target_sub2api_version=""
resolved_version=""
resolved_release_url=""
env_changed=0
upgraded_services=""

compose() {
  docker compose \
    --project-directory "$compose_project_dir" \
    -f "$compose_file" \
    --env-file "$env_file" \
    "$@"
}

usage() {
  cat >&2 <<'EOF'
用法:
  upgrade-service.sh <服务> [目标版本]
  upgrade-service.sh --check <服务>

服务:
  device_platform  设备平台
  voice_gateway    语音网关
  admin_web        品牌管理端
  sub2api          品牌 AI 网关
  all              按 sub2api、device_platform、voice_gateway、admin_web 顺序升级

示例:
  ./scripts/upgrade-service.sh --check all
  ./scripts/upgrade-service.sh admin_web 0.10.0
  ./scripts/upgrade-service.sh sub2api

--check 只读取 GitHub Releases，不修改 .env，也不会重启容器。
基础设施 postgres、redis、mqtt 和 download_* 不在自动升级范围内。
EOF
}

require_command() {
  command_name="$1"
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "缺少命令: $command_name" >&2
    exit 1
  fi
}

validate_service() {
  service="$1"
  case "$service" in
    device_platform|voice_gateway|admin_web|sub2api|all)
      return 0
      ;;
    *)
      echo "该服务由部署配置统一维护，不能单独升级: $service" >&2
      return 1
      ;;
  esac
}

validate_version() {
  version="$1"
  printf '%s' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'
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

require_env_key() {
  key="$1"
  if ! grep -q "^${key}=" "$env_file"; then
    echo ".env 缺少必要变量: $key" >&2
    exit 1
  fi
}

service_repository() {
  service="$1"
  case "$service" in
    sub2api)
      printf '%s\n' "$sub2api_repository"
      ;;
    *)
      printf '%s\n' "$platform_repository"
      ;;
  esac
}

service_image_name() {
  service="$1"
  case "$service" in
    sub2api)
      printf '%s\n' "ghcr.io/tissyboxc/sub2api"
      ;;
    device_platform)
      printf '%s\n' "ghcr.io/tissyboxc/sprout-device-platform"
      ;;
    voice_gateway)
      printf '%s\n' "ghcr.io/tissyboxc/sprout-voice-gateway"
      ;;
    admin_web)
      printf '%s\n' "ghcr.io/tissyboxc/sprout-admin-web"
      ;;
    *)
      return 1
      ;;
  esac
}

# The running image is authoritative: .env may be ahead of the actual container
# after an interrupted upgrade or a manual rollback.
read_running_image_version() {
  service="$1"
  container_id="$(compose ps -q "$service" 2>/dev/null || true)"
  if [ -z "$container_id" ]; then
    printf '%s\n' "unknown"
    return 0
  fi

  image_tag="$(
    docker inspect --format '{{.Config.Image}}' "$container_id" 2>/dev/null |
      awk -F: 'NF > 1 { print $NF }' || true
  )"
  if validate_version "$image_tag"; then
    printf '%s\n' "$image_tag"
  else
    printf '%s\n' "unknown"
  fi
}

resolve_latest_release() {
  repository="$1"
  response_file="$(mktemp)"
  curl_args=(
    --fail
    --silent
    --show-error
    --location
    --max-time 20
    --header "Accept: application/vnd.github+json"
    --header "User-Agent: sprout-upgrade-service"
  )
  if [ -n "$github_token" ]; then
    # The token stays in the process environment and is never printed.
    curl_args+=(--header "Authorization: Bearer ${github_token}")
  fi

  if ! curl "${curl_args[@]}" \
    "${github_api_base}/repos/${repository}/releases/latest" \
    --output "$response_file"; then
    rm -f "$response_file"
    return 1
  fi

  release_tag="$(jq -r '.tag_name // empty' "$response_file" 2>/dev/null || true)"
  resolved_release_url="$(jq -r '.html_url // empty' "$response_file" 2>/dev/null || true)"
  rm -f "$response_file"

  resolved_version="${release_tag#v}"
  if ! validate_version "$resolved_version"; then
    resolved_version=""
    resolved_release_url=""
    return 1
  fi
}

# Resolve one immutable release tag requested by the backend. The worker must
# never silently replace a queued target with a newer release.
resolve_release_by_version() {
  repository="$1"
  requested_version="$2"
  for candidate_tag in "v${requested_version}" "${requested_version}"; do
    response_file="$(mktemp)"
    curl_args=(
      --fail
      --silent
      --show-error
      --location
      --max-time 20
      --header "Accept: application/vnd.github+json"
      --header "User-Agent: sprout-upgrade-service"
    )
    if [ -n "$github_token" ]; then
      curl_args+=(--header "Authorization: Bearer ${github_token}")
    fi

    if curl "${curl_args[@]}" \
      "${github_api_base}/repos/${repository}/releases/tags/${candidate_tag}" \
      --output "$response_file" 2>/dev/null; then
      release_tag="$(jq -r '.tag_name // empty' "$response_file" 2>/dev/null || true)"
      release_url="$(jq -r '.html_url // empty' "$response_file" 2>/dev/null || true)"
      rm -f "$response_file"
      resolved_version="${release_tag#v}"
      if [ "$resolved_version" = "$requested_version" ] &&
        validate_version "$resolved_version"; then
        resolved_release_url="$release_url"
        return 0
      fi
    else
      rm -f "$response_file"
    fi
  done

  resolved_version=""
  resolved_release_url=""
  return 1
}

release_version_is_published() {
  service="$1"
  requested_version="$2"
  repository="$(service_repository "$service")"
  resolve_release_by_version "$repository" "$requested_version"
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

emit_check_json() {
  service="$1"
  repository="$2"
  checked_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  current_version="$(read_running_image_version "$service")"

  if resolve_latest_release "$repository"; then
    latest_version="$resolved_version"
    release_url="$resolved_release_url"
    if [ "$current_version" = "unknown" ]; then
      status="unknown"
    elif [ "$current_version" = "$latest_version" ]; then
      status="current"
    elif version_is_older "$current_version" "$latest_version"; then
      status="outdated"
    else
      status="current"
    fi
  else
    latest_version=""
    release_url=""
    status="unknown"
  fi

  jq -n \
    --arg id "$service" \
    --arg current_version "$current_version" \
    --arg latest_version "$latest_version" \
    --arg status "$status" \
    --arg release_url "$release_url" \
    --arg checked_at "$checked_at" \
    '{
      id: $id,
      current_version: $current_version,
      latest_version: $latest_version,
      status: $status,
      release_url: $release_url,
      checked_at: $checked_at
    }'
}

check_services() {
  requested_service="$1"
  require_command curl
  require_command jq
  require_command docker

  if [ "$requested_service" = "all" ]; then
    check_file="$(mktemp)"
    for service in $upgrade_order; do
      emit_check_json "$service" "$(service_repository "$service")" >> "$check_file"
    done
    jq -s \
      --arg checked_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      '{services: ., checked_at: $checked_at}' \
      "$check_file"
    rm -f "$check_file"
    return 0
  fi

  emit_check_json "$requested_service" "$(service_repository "$requested_service")"
}

assert_git_clean() {
  require_command git
  if ! git -C "$cloud_dir" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "云端部署目录不是 Git 工作区，拒绝升级。" >&2
    exit 1
  fi

  # Generated migration data and certificates are runtime artifacts, not
  # source changes. Everything else must be clean before a release image is
  # allowed to replace a running container.
  git_status="$(
    git -C "$cloud_dir" status --porcelain --untracked-files=all -- . |
      grep -vE '^(\?\?|!!) (migration-data|mosquitto/certs)/' || true
  )"
  if [ -n "$git_status" ]; then
    echo "云端部署目录存在未提交变更，拒绝升级。请先提交或清理：" >&2
    printf '%s\n' "$git_status" >&2
    exit 1
  fi
}

assert_compose_config() {
  require_command docker
  if ! compose config >/dev/null; then
    echo "docker compose 配置校验失败，拒绝升级。" >&2
    return 1
  fi
}

acquire_upgrade_lock() {
  lock_file="${TMPDIR:-/tmp}/sprout-upgrade-service.lock"
  if command -v flock >/dev/null 2>&1; then
    exec 9>"$lock_file"
    if ! flock -n 9; then
      echo "已有另一个服务升级任务正在执行。" >&2
      exit 1
    fi
    return 0
  fi

  lock_dir="${lock_file}.d"
  if ! mkdir "$lock_dir" 2>/dev/null; then
    echo "已有另一个服务升级任务正在执行。" >&2
    exit 1
  fi
  trap 'rmdir "$lock_dir" 2>/dev/null || true' EXIT
}

write_env_versions() {
  platform_target="$1"
  sub2api_target="$2"
  temp_file="$(mktemp "${env_file}.tmp.XXXXXX")"

  if ! awk \
    -v platform_key="SPROUT_PLATFORM_VERSION" \
    -v platform_target="$platform_target" \
    -v sub2api_key="SPROUT_SUB2API_VERSION" \
    -v sub2api_target="$sub2api_target" '
      BEGIN {
        platform_found = 0
        sub2api_found = 0
        platform_requested = (platform_target != "")
        sub2api_requested = (sub2api_target != "")
      }
      index($0, platform_key "=") == 1 {
        if (platform_requested) {
          print platform_key "=" platform_target
        } else {
          print
        }
        platform_found = 1
        next
      }
      index($0, sub2api_key "=") == 1 {
        if (sub2api_requested) {
          print sub2api_key "=" sub2api_target
        } else {
          print
        }
        sub2api_found = 1
        next
      }
      {
        print
      }
      END {
        if ((platform_requested && !platform_found) || (sub2api_requested && !sub2api_found)) {
          exit 1
        }
      }
    ' "$env_file" > "$temp_file"; then
    rm -f "$temp_file"
    echo ".env 版本写入失败；文件未被替换。" >&2
    return 1
  fi

  chmod 600 "$temp_file"
  mv "$temp_file" "$env_file"
}

pre_pull_images() {
  pull_platform="$1"
  pull_sub2api="$2"

  if [ "$pull_platform" = "1" ]; then
    SPROUT_PLATFORM_VERSION="$target_platform_version" \
      SPROUT_SUB2API_VERSION="$previous_sub2api_version" \
      compose pull device_platform voice_gateway admin_web
  fi

  if [ "$pull_sub2api" = "1" ]; then
    SPROUT_PLATFORM_VERSION="$previous_platform_version" \
      SPROUT_SUB2API_VERSION="$target_sub2api_version" \
      compose pull sub2api
  fi
}

wait_for_service() {
  service="$1"
  deadline=$((SECONDS + health_timeout))

  while [ "$SECONDS" -lt "$deadline" ]; do
    container_id="$(
      compose ps -q "$service" 2>/dev/null || true
    )"
    if [ -n "$container_id" ]; then
      health_status="$(
        docker inspect \
          --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' \
          "$container_id" 2>/dev/null || true
      )"
      case "$health_status" in
        healthy|running)
          echo "服务已就绪: $service"
          return 0
          ;;
        exited|dead)
          echo "服务启动失败: $service ($health_status)" >&2
          return 1
          ;;
      esac
    fi
    sleep 2
  done

  echo "服务未在 ${health_timeout} 秒内就绪: $service" >&2
  return 1
}

rollback_on_error() {
  exit_code="$1"
  trap - ERR
  set +e

  echo "服务升级失败，开始尝试恢复旧版本。" >&2
  if [ "$env_changed" -eq 1 ]; then
    if write_env_versions "$previous_platform_version" "$previous_sub2api_version"; then
      for service in $upgraded_services; do
        echo "恢复服务: $service" >&2
        SPROUT_PLATFORM_VERSION="$previous_platform_version" \
          SPROUT_SUB2API_VERSION="$previous_sub2api_version" \
          compose pull "$service" >/dev/null 2>&1 || true
        SPROUT_PLATFORM_VERSION="$previous_platform_version" \
          SPROUT_SUB2API_VERSION="$previous_sub2api_version" \
          compose up -d --no-deps "$service" >/dev/null 2>&1 || true
      done
    else
      echo "旧版本号写回失败，请人工恢复 .env。" >&2
    fi
  fi

  echo "服务升级未完成，退出码: $exit_code" >&2
  exit "$exit_code"
}

resolve_target_for_service() {
  service="$1"
  requested_target_version="$2"
  repository="$(service_repository "$service")"
  if [ -n "$requested_target_version" ]; then
    if ! resolve_release_by_version "$repository" "$requested_target_version"; then
      echo "无法确认 ${service} 的目标发布版本 ${requested_target_version}，升级已停止。" >&2
      return 1
    fi
  elif ! resolve_latest_release "$repository"; then
    echo "无法读取 ${service} 的最新发布版本，升级已停止。" >&2
    return 1
  fi

  case "$service" in
    sub2api)
      target_sub2api_version="$resolved_version"
      ;;
    *)
      target_platform_version="$resolved_version"
      ;;
  esac
}

upgrade_services() {
  requested_service="$1"
  requested_target_version="${2:-}"
  requested_services="$requested_service"
  if [ "$requested_service" = "all" ]; then
    requested_services="$upgrade_order"
  fi

  require_command docker
  require_command git
  require_command jq
  require_command curl
  require_command awk

  if [ ! -f "$env_file" ]; then
    echo "缺少云端环境文件: $env_file" >&2
    exit 1
  fi
  if [ ! -f "$compose_file" ]; then
    echo "缺少 Compose 文件: $compose_file" >&2
    exit 1
  fi
  if ! printf '%s' "$health_timeout" | grep -Eq '^[0-9]+$' || [ "$health_timeout" -lt 10 ]; then
    echo "SPROUT_UPGRADE_HEALTH_TIMEOUT_SECONDS 必须是大于等于 10 的整数。" >&2
    exit 1
  fi

  require_env_key "SPROUT_PLATFORM_VERSION"
  require_env_key "SPROUT_SUB2API_VERSION"
  previous_platform_version="$(read_env_value SPROUT_PLATFORM_VERSION)"
  previous_sub2api_version="$(read_env_value SPROUT_SUB2API_VERSION)"

  if ! validate_version "$previous_platform_version"; then
    echo ".env 中的 SPROUT_PLATFORM_VERSION 格式无效。" >&2
    exit 1
  fi
  if ! validate_version "$previous_sub2api_version"; then
    echo ".env 中的 SPROUT_SUB2API_VERSION 格式无效。" >&2
    exit 1
  fi

  assert_git_clean
  "$cloud_dir/scripts/validate-cloud-env.sh" "$env_file"
  assert_compose_config

  for service in $requested_services; do
    target_version_arg=""
    if [ "$requested_service" != "all" ]; then
      target_version_arg="$requested_target_version"
    fi
    resolve_target_for_service "$service" "$target_version_arg"
  done

  services_to_upgrade=""
  for service in $requested_services; do
    current_version="$(read_running_image_version "$service")"
    target_version="$target_platform_version"
    if [ "$service" = "sub2api" ]; then
      target_version="$target_sub2api_version"
    fi

    if [ "$current_version" = "unknown" ]; then
      echo "无法读取 $service 当前版本，升级已停止。" >&2
      exit 1
    fi
    # An explicit target is an operator-approved version change. It may be a
    # reinstall or rollback, but only a published release is accepted.
    if version_is_older "$target_version" "$current_version" &&
      ! release_version_is_published "$service" "$target_version"; then
      echo "目标版本 ${target_version} 不是 ${service} 的已发布版本，拒绝降级。" >&2
      exit 1
    fi
    if [ "$current_version" != "$target_version" ]; then
      services_to_upgrade="$services_to_upgrade $service"
    fi
  done

  if [ -z "$services_to_upgrade" ]; then
    echo "所有选中的服务已经是最新版本。"
    return 0
  fi

  acquire_upgrade_lock
  trap 'rollback_on_error "$?"' ERR

  pull_platform=0
  pull_sub2api=0
  for service in $services_to_upgrade; do
    if [ "$service" = "sub2api" ]; then
      pull_sub2api=1
    else
      pull_platform=1
    fi
  done

  # Pull before changing .env so a missing or unauthorized image cannot leave
  # the deployment file pointing at a version that was never downloaded.
  pre_pull_images "$pull_platform" "$pull_sub2api"

  new_platform_target=""
  new_sub2api_target=""
  for service in $services_to_upgrade; do
    if [ "$service" = "sub2api" ]; then
      new_sub2api_target="$target_sub2api_version"
    else
      new_platform_target="$target_platform_version"
    fi
  done

  write_env_versions "$new_platform_target" "$new_sub2api_target"
  env_changed=1
  echo "版本已原子切换: platform=${new_platform_target:-unchanged} sub2api=${new_sub2api_target:-unchanged}"

  for service in $services_to_upgrade; do
    # --no-deps is deliberate: an upgrade must only replace the selected
    # service and must not restart databases, caches, or the upgrade worker.
    # Record the attempt before Compose runs so a failed service is included
    # in rollback rather than being left on the new image.
    upgraded_services="$upgraded_services $service"
    compose up -d --no-deps "$service"
    wait_for_service "$service"
  done

  trap - ERR
  echo "服务升级完成: $(printf '%s' "$upgraded_services" | sed 's/^ //')"
}

main() {
  if [ "$#" -eq 2 ] && [ "$1" = "--check" ]; then
    validate_service "$2"
    check_services "$2"
    return 0
  fi

  case "$#" in
    1)
      validate_service "$1"
      upgrade_services "$1" ""
      ;;
    2)
      validate_service "$1"
      if [ "$1" = "all" ]; then
        echo "批量升级不接受单一目标版本；请逐个服务传入版本。" >&2
        usage
        exit 1
      fi
      if ! validate_version "$2"; then
        echo "目标版本格式无效: $2" >&2
        exit 1
      fi
      upgrade_services "$1" "$2"
      ;;
    *)
      usage
      exit 1
      ;;
  esac
}

main "$@"
