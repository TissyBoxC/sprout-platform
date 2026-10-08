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

if ! grep -Eq 'test -s .*status\.json' "$compose_file"; then
  echo "upgrade_worker 缺少 status.json 健康检查。" >&2
  exit 1
fi

echo "service-upgrade-worker shell contract passed."
