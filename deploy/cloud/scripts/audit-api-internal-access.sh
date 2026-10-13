#!/usr/bin/env bash
# Keep the device platform's /internal API private at the 1Panel edge.
#
# The API site proxy is normally `location ^~ / { proxy_pass ... }`, which
# forwards every path to the device platform. The release publication endpoints
# have public `/api/v1/release-publication/` aliases, so `/internal/` must not be
# reachable through the public API hostname.
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
default_www_dir="/opt/1panel/www"
www_dir="${SPROUT_1PANEL_WWW_DIR:-$default_www_dir}"
site_name="${SPROUT_API_SITE_NAME:-api.clarkhub.cn}"
target_name="internal-access.conf"

usage() {
  cat >&2 <<'EOF'
用法:
  audit-api-internal-access.sh
  audit-api-internal-access.sh --apply

作用:
  检查 1Panel 的 API 站点是否在公网入口拒绝 /internal。
  默认只读，不修改文件。

--apply:
  在缺少拦截规则时写入 internal-access.conf，备份原文件，并通过当前
  OpenResty 容器执行 nginx -t 和 reload；失败时自动回滚。

可通过以下环境变量覆盖默认值:
  SPROUT_1PANEL_WWW_DIR  1Panel 站点根目录，默认 /opt/1panel/www
  SPROUT_API_SITE_NAME   API 站点目录名，默认 api.clarkhub.cn
EOF
}

if [ "${1:-}" = "--help" ] || [ "${1:-}" = "-h" ]; then
  usage
  exit 0
fi

if [ "$#" -gt 1 ] || { [ "$#" -eq 1 ] && [ "$1" != "--apply" ]; }; then
  usage
  exit 1
fi

apply=false
if [ "${1:-}" = "--apply" ]; then
  apply=true
fi

require_command() {
  command_name="$1"
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "缺少命令: $command_name" >&2
    exit 1
  fi
}

require_command awk
require_command chmod
require_command chown
require_command cp
require_command dirname
require_command find
require_command grep
require_command mkdir
require_command mktemp
require_command mv
require_command sort

site_dir="$www_dir/sites/$site_name"
proxy_dir="$site_dir/proxy"
root_config="$proxy_dir/root.conf"
target_config="$proxy_dir/$target_name"

if [ ! -d "$proxy_dir" ]; then
  echo "缺少 API 站点代理目录: $proxy_dir" >&2
  exit 1
fi

if [ ! -f "$root_config" ]; then
  echo "缺少 API 站点根代理配置: $root_config" >&2
  exit 1
fi

has_internal_deny() {
  file="$1"
  awk '
    BEGIN { location = 0; return_404 = 0 }
    /^[[:space:]]*location[[:space:]]+\^~[[:space:]]+\/internal([[:space:]\/{]|$)/ {
      location = 1
      return_404 = 0
      next
    }
    location && /^[[:space:]]*return[[:space:]]+404[[:space:]]*;/ {
      return_404 = 1
    }
    location && /^[[:space:]]*}/ {
      if (return_404) {
        exit 0
      }
      location = 0
      return_404 = 0
    }
    END {
      if (location && return_404) {
        exit 0
      }
      exit 1
    }
  ' "$file"
}

proxy_files="$(
  find "$proxy_dir" -maxdepth 1 -type f -name '*.conf' 2>/dev/null | sort
)"
if [ -z "$proxy_files" ]; then
  echo "未找到 API 站点代理配置: $proxy_dir/*.conf" >&2
  exit 1
fi

internal_deny_file=""
while IFS= read -r file; do
  [ -n "$file" ] || continue
  if has_internal_deny "$file"; then
    internal_deny_file="$file"
    break
  fi
done <<EOF
$proxy_files
EOF

if [ -n "$internal_deny_file" ]; then
  echo "API 站点已拦截公网 /internal 访问: $internal_deny_file"
  exit 0
fi

echo "API 站点没有在公网入口拒绝 /internal: $site_dir" >&2

if [ "$apply" != true ]; then
  echo "只读审计完成；使用 --apply 写入 $target_config。" >&2
  exit 1
fi

backup_dir="$www_dir/.codex-backups/$(date -u +%Y%m%d-%H%M%S)-api-internal-access"
mkdir -p -- "$backup_dir"

backup_file=""
if [ -f "$target_config" ]; then
  backup_file="$backup_dir/$target_name"
  cp -p -- "$target_config" "$backup_file"
fi

temp_config="$(mktemp "${target_config}.codex-internal.XXXXXX")"
cat > "$temp_config" <<'EOF'
# Managed by deploy/cloud/scripts/audit-api-internal-access.sh.
# The public release pipeline uses /api/v1/release-publication/ aliases.
location ^~ /internal {
    return 404;
}
EOF

chmod --reference="$root_config" "$temp_config" 2>/dev/null || chmod 0644 "$temp_config"
chown --reference="$root_config" "$temp_config" 2>/dev/null || true
mv -- "$temp_config" "$target_config"

rollback() {
  status="$1"
  if [ -n "$backup_file" ] && [ -f "$backup_file" ]; then
    cp -p -- "$backup_file" "$target_config"
  else
    rm -f -- "$target_config"
  fi
  echo "API 内部路径拦截修复失败，已回滚。" >&2
  exit "$status"
}

if ! command -v docker >/dev/null 2>&1; then
  rollback 1
fi

openresty_container="$(
  docker ps --format '{{.Names}}' | grep -E '^1Panel-openresty-' | head -n 1
)"
if [ -z "$openresty_container" ]; then
  rollback 1
fi

if ! docker exec "$openresty_container" openresty -t; then
  rollback 1
fi

if ! docker exec "$openresty_container" openresty -s reload; then
  rollback 1
fi

echo "已拦截 API 公网 /internal 访问并通过 nginx -t；OpenResty 已重载。"
echo "配置: $target_config"
echo "备份: $backup_dir"
