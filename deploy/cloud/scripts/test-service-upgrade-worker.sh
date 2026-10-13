#!/usr/bin/env bash
# Guard the worker's shell contract before publishing a release image.
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
worker_script="$script_dir/service-upgrade-worker.sh"
compose_file="$script_dir/../docker-compose.yml"

bash -n "$worker_script"

if ! grep -Eq '^require_command\(\)' "$worker_script"; then
  echo "service-upgrade-worker.sh 缺少 require_command 定义。" >&2
  exit 1
fi

if ! grep -Eq '^release_catalog_is_valid\(\)' "$worker_script"; then
  echo "service-upgrade-worker.sh 缺少版本目录有效性校验。" >&2
  exit 1
fi

if ! grep -Eq '^refresh_release_catalog\(\)' "$worker_script"; then
  echo "service-upgrade-worker.sh 缺少版本目录刷新与退避逻辑。" >&2
  exit 1
fi

resolve_latest_block="$(
  sed -n '/^resolve_latest_release()/,/^}/p' "$worker_script"
)"
if ! printf '%s\n' "$resolve_latest_block" | grep -Eq 'cached_version'; then
  echo "resolve_latest_release 缺少 GitHub 失败时的缓存回退。" >&2
  exit 1
fi

if ! grep -Eq '^platform_repository=' "$worker_script" ||
  ! grep -Eq '^sub2api_repository=' "$worker_script"; then
  echo "service-upgrade-worker.sh 缺少发布仓库配置。" >&2
  exit 1
fi

validate_service_block="$(
  sed -n '/^validate_service()/,/^}/p' "$worker_script"
)"
for unsupported_service in \
  postgres \
  redis \
  mqtt \
  download_init \
  download_ftp \
  download_http; do
  if printf '%s\n' "$validate_service_block" |
    grep -Eq "(^|[|])${unsupported_service}([|]|$)"; then
    echo "validate_service 不应接受基础设施服务: $unsupported_service" >&2
    exit 1
  fi
done

if ! grep -Eq 'status=.*status\.json' "$compose_file" ||
  ! grep -Eq 'generated=.*jq .*generated_at' "$compose_file" ||
  ! grep -Eq 'age.*-le 180' "$compose_file"; then
  echo "upgrade_worker 缺少 status.json 新鲜度健康检查。" >&2
  exit 1
fi

if grep -Eq 'python3' "$compose_file"; then
  echo "upgrade_worker 健康检查依赖容器内不存在的 python3。" >&2
  exit 1
fi

# read_running_image_tag must strip an @sha256 digest before extracting the
# tag; otherwise digest-pinned images surface the digest as the version.
read_tag_block="$(
  sed -n '/^read_running_image_tag()/,/^}/p' "$worker_script"
)"
if ! printf '%s\n' "$read_tag_block" | grep -Eq "awk -F@"; then
  echo "read_running_image_tag 未剥离 @sha256 摘要，版本号会被误读。" >&2
  exit 1
fi

resolved_tag="$(
  printf '%s\n' 'nginx:1.27-alpine@sha256:00000000000000000000000000000000' |
    awk -F@ '{ print $1 }' |
    awk -F: 'NF > 1 { print $NF }'
)"
if [ "$resolved_tag" != "1.27-alpine" ]; then
  echo "read_running_image_tag 摘要剥离结果不正确: $resolved_tag" >&2
  exit 1
fi

echo "service-upgrade-worker shell contract passed."
