#!/usr/bin/env bash
# Audit and repair response header inheritance in 1Panel proxy snippets.
#
# 1Panel writes `add_header X-Cache $upstream_cache_status;` inside each
# generated location block. Nginx does not merge add_header directives from a
# parent context once a child context has any add_header, so the server-level
# HSTS header is dropped from normal proxy responses. This script adds the
# missing HSTS header to the same location and makes X-Cache apply to errors.
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
default_www_dir="/opt/1panel/www"
www_dir="${SPROUT_1PANEL_WWW_DIR:-$default_www_dir}"

usage() {
  cat >&2 <<'EOF'
用法:
  audit-1panel-proxy-headers.sh
  audit-1panel-proxy-headers.sh --apply

作用:
  检查 1Panel 生成的站点代理片段是否会覆盖 server 级 HSTS。
  默认只读，不修改文件。

--apply:
  先备份受影响的 proxy/*.conf，再补齐 HSTS，并让 X-Cache 在所有响应上
  生效。修改后调用同一个 OpenResty 容器执行 nginx -t；失败时自动回滚。
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
require_command dirname
require_command find
require_command grep
require_command mktemp
require_command sed

if [ ! -d "$www_dir/sites" ]; then
  echo "缺少 1Panel 站点目录: $www_dir/sites" >&2
  exit 1
fi

proxy_files="$(
  find "$www_dir/sites" -path '*/proxy/*.conf' -type f 2>/dev/null | sort
)"
if [ -z "$proxy_files" ]; then
  echo "未找到 1Panel 站点代理片段: $www_dir/sites/*/proxy/*.conf" >&2
  exit 1
fi

header_needs_fix() {
  file="$1"
  awk '
    BEGIN { location = 0; cache_needs_always = 0; has_hsts = 0 }
    /^[[:space:]]*location[[:space:]]/ {
      if (location && (!has_hsts || cache_needs_always)) {
        exit 10
      }
      location = 1
      cache_needs_always = 0
      has_hsts = 0
    }
    location && /^[[:space:]]*add_header[[:space:]]+X-Cache[[:space:]]/ {
      if ($0 !~ /always;/) {
        cache_needs_always = 1
      }
    }
    location && /^[[:space:]]*add_header[[:space:]]+Strict-Transport-Security[[:space:]]/ {
      has_hsts = 1
    }
    location && /^[[:space:]]*}$/ {
      if (!has_hsts || cache_needs_always) {
        exit 10
      }
      location = 0
      cache_needs_always = 0
      has_hsts = 0
    }
    END {
      if (location && (!has_hsts || cache_needs_always)) {
        exit 10
      }
    }
  ' "$file"
}

fix_header_block() {
  file="$1"
  temp_file="$(mktemp "${file}.codex-headers.XXXXXX")"
  if ! awk '
    BEGIN { location = 0; has_hsts = 0; changed = 0 }
    /^[[:space:]]*location[[:space:]]/ {
      if (location && !has_hsts) {
        print "    add_header Strict-Transport-Security \"max-age=31536000; includeSubDomains\" always;"
        changed = 1
      }
      location = 1
      has_hsts = 0
      print
      next
    }
    location && /^[[:space:]]*add_header[[:space:]]+X-Cache[[:space:]]/ {
      if ($0 !~ /always;/) {
        sub(/;[[:space:]]*$/, " always;", $0)
        changed = 1
      }
      print
      next
    }
    location && /^[[:space:]]*add_header[[:space:]]+Strict-Transport-Security[[:space:]]/ {
      has_hsts = 1
      print
      next
    }
    location && /^[[:space:]]*}$/ {
      if (!has_hsts) {
        print "    add_header Strict-Transport-Security \"max-age=31536000; includeSubDomains\" always;"
        changed = 1
      }
      print
      location = 0
      has_hsts = 0
      next
    }
    { print }
    END {
      if (location && !has_hsts) {
        print "    add_header Strict-Transport-Security \"max-age=31536000; includeSubDomains\" always;"
        changed = 1
      }
      if (!changed) {
        exit 20
      }
    }
  ' "$file" > "$temp_file"; then
    rm -f -- "$temp_file"
    return 1
  fi

  chmod --reference="$file" "$temp_file" 2>/dev/null || true
  chown --reference="$file" "$temp_file" 2>/dev/null || true
  mv -- "$temp_file" "$file"
}

changed_files=""
changed_list="$(mktemp)"
trap 'rm -f -- "$changed_list"' EXIT
if [ "$apply" = true ]; then
  backup_dir="$www_dir/.codex-backups/$(date -u +%Y%m%d-%H%M%S)-proxy-headers"
fi

while IFS= read -r file; do
  [ -n "$file" ] || continue
  if ! header_needs_fix "$file"; then
    printf '%s\n' "$file" >> "$changed_list"
  fi
done <<EOF
$proxy_files
EOF

if [ ! -s "$changed_list" ]; then
  echo "1Panel 代理片段响应头配置正确，无需修改。"
  exit 0
fi

echo "发现需要补齐 HSTS 的 1Panel 代理片段:"
while IFS= read -r file <&3; do
  [ -n "$file" ] || continue
  echo "  - $file"
done 3< "$changed_list"

if [ "$apply" != true ]; then
  echo "只读审计完成；使用 --apply 应用修复。"
  exit 1
fi

mkdir -p -- "$backup_dir"
while IFS= read -r file <&3; do
  [ -n "$file" ] || continue
  relative="${file#"$www_dir"/}"
  destination="$backup_dir/$relative"
  mkdir -p -- "$(dirname -- "$destination")"
  cp -p -- "$file" "$destination"
done 3< "$changed_list"

rollback() {
  status="$1"
  while IFS= read -r file <&3; do
    [ -n "$file" ] || continue
    relative="${file#"$www_dir"/}"
    backup="$backup_dir/$relative"
    if [ -f "$backup" ]; then
      cp -p -- "$backup" "$file"
    fi
  done 3< "$changed_list"
  echo "响应头修复失败，已从 $backup_dir 回滚。" >&2
  exit "$status"
}

while IFS= read -r file <&3; do
  [ -n "$file" ] || continue
  if ! fix_header_block "$file"; then
    rollback 1
  fi
done 3< "$changed_list"

if command -v docker >/dev/null 2>&1; then
  openresty_container="$(
    docker ps --format '{{.Names}}' | grep -E '^1Panel-openresty-' | head -n 1
  )"
  if [ -z "$openresty_container" ]; then
    rollback 1
  fi
  if ! docker exec "$openresty_container" openresty -t; then
    rollback 1
  fi
  docker exec "$openresty_container" openresty -s reload
else
  echo "未找到 docker 命令，无法自动执行 nginx -t 和 reload；已保留备份。" >&2
  exit 1
fi

echo "已补齐 HSTS 并通过 nginx -t；OpenResty 已重载。备份: $backup_dir"
