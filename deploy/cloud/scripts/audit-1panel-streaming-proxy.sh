#!/usr/bin/env bash
# Audit and repair buffering/timeouts for streaming reverse proxies.
#
# Sub2API streams model output, and the voice gateway keeps WebSocket sessions
# open. Nginx buffers responses and closes idle upstream connections by default,
# which makes those endpoints look intermittently broken. The script is
# read-only unless --apply is passed.
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
default_www_dir="/opt/1panel/www"
www_dir="${SPROUT_1PANEL_WWW_DIR:-$default_www_dir}"

usage() {
  cat >&2 <<'EOF'
用法:
  audit-1panel-streaming-proxy.sh
  audit-1panel-streaming-proxy.sh --apply

检查 Sub2API 根代理和语音 WebSocket 的缓冲、长连接超时配置。
默认只读；--apply 会先备份，再修改并通过 openresty -t 校验。
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

sub_root="$www_dir/sites/sub.clarkhub.cn/proxy/root.conf"
voice_root="$www_dir/sites/voice.clarkhub.cn/proxy/root.conf"
voice_site="$www_dir/conf.d/voice.clarkhub.cn.conf"

for file in "$sub_root" "$voice_root" "$voice_site"; do
  if [ ! -f "$file" ]; then
    echo "缺少 1Panel 站点配置: $file" >&2
    exit 1
  fi
done

sub_needs_fix=false
if ! awk '
  /^[[:space:]]*location[[:space:]]/ { location = 1 }
  location && /^[[:space:]]*proxy_buffering[[:space:]]+off[[:space:]]*;/ { buffering = 1 }
  location && /^[[:space:]]*proxy_request_buffering[[:space:]]+off[[:space:]]*;/ { request_buffering = 1 }
  location && /^[[:space:]]*proxy_read_timeout[[:space:]]+3600s[[:space:]]*;/ { read_timeout = 1 }
  location && /^[[:space:]]*proxy_send_timeout[[:space:]]+3600s[[:space:]]*;/ { send_timeout = 1 }
  location && /^[[:space:]]*send_timeout[[:space:]]+3600s[[:space:]]*;/ { send = 1 }
  END { exit !(location && buffering && request_buffering && read_timeout && send_timeout && send) }
' "$sub_root"; then
  sub_needs_fix=true
fi

voice_needs_fix=false
if ! awk '
  /^[[:space:]]*location[[:space:]]+\^~[[:space:]]+\/v1\/voice/ { voice = 1 }
  voice && /^[[:space:]]*proxy_buffering[[:space:]]+off[[:space:]]*;/ { buffering = 1 }
  voice && /^[[:space:]]*proxy_request_buffering[[:space:]]+off[[:space:]]*;/ { request_buffering = 1 }
  voice && /^[[:space:]]*proxy_read_timeout[[:space:]]+3600s[[:space:]]*;/ { read_timeout = 1 }
  voice && /^[[:space:]]*proxy_send_timeout[[:space:]]+3600s[[:space:]]*;/ { send_timeout = 1 }
  voice && /^[[:space:]]*send_timeout[[:space:]]+3600s[[:space:]]*;/ { send = 1 }
  END { exit !(voice && buffering && request_buffering && read_timeout && send_timeout && send) }
' "$voice_site"; then
  voice_needs_fix=true
fi

if [ "$voice_needs_fix" = false ]; then
  if ! grep -Fq 'proxy_set_header Connection "upgrade";' "$voice_site"; then
    voice_needs_fix=true
  fi
fi

if [ "$sub_needs_fix" = false ] && [ "$voice_needs_fix" = false ]; then
  grep -Eq '^[[:space:]]*location[[:space:]]+\^~[[:space:]]+/[[:space:]]*\{' \
    "$sub_root"
  grep -Eq '^[[:space:]]*location[[:space:]]+\^~[[:space:]]+/v1/voice' \
    "$voice_site"
fi

printf '%s\n' '=== streaming proxy audit ==='
printf 'sub2api_root=%s\n' "$([ "$sub_needs_fix" = false ] && echo ok || echo needs_fix)"
printf 'voice_websocket=%s\n' "$([ "$voice_needs_fix" = false ] && echo ok || echo needs_fix)"

if [ "$sub_needs_fix" = false ] && [ "$voice_needs_fix" = false ]; then
  echo "流式代理配置正确，无需修改。"
  exit 0
fi
if [ "$apply" != true ]; then
  echo "只读审计完成；使用 --apply 应用修复。" >&2
  exit 1
fi

backup_dir="$www_dir/.codex-backups/streaming-proxy-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$backup_dir"
changed_files="$backup_dir/changed-files"
: >"$changed_files"

backup_file() {
  file="$1"
  relative="${file#"$www_dir"/}"
  destination="$backup_dir/$relative"
  mkdir -p "$(dirname "$destination")"
  cp -p "$file" "$destination"
  printf '%s\n' "$file" >>"$changed_files"
}

rollback() {
  while IFS= read -r file; do
    [ -n "$file" ] || continue
    relative="${file#"$www_dir"/}"
    cp -p "$backup_dir/$relative" "$file"
  done <"$changed_files"
  echo "流式代理修复失败，已从 $backup_dir 回滚。" >&2
  exit 1
}

sync_parent_directive() {
  file="$1"
  parent_directive="$2"
  temp_file="$(mktemp "${file}.codex-parent.XXXXXX")"
  if ! awk -v directive="$parent_directive" '
    BEGIN { voice = 0; child = 0 }
    /^[[:space:]]*location[[:space:]]+\^~[[:space:]]+\/v1\/voice/ { voice = 1 }
    voice && !child && /^[[:space:]]*location[[:space:]]/ {
      print directive
      child = 1
    }
    voice && /^[[:space:]]*}$/ { voice = 0; child = 0 }
    { print }
  ' "$file" >"$temp_file"; then
    rm -f "$temp_file"
    return 1
  fi
  chmod --reference="$file" "$temp_file" 2>/dev/null || true
  mv -- "$temp_file" "$file"
}

add_sub2api_streaming() {
  file="$1"
  temp_file="$(mktemp "${file}.codex-stream.XXXXXX")"
  if ! awk '
    /^[[:space:]]*location[[:space:]]+\^~[[:space:]]+\/[[:space:]]*\{/ {
      in_root = 1
    }
    in_root && !added && /^[[:space:]]*}/ {
      print "    proxy_buffering off;"
      print "    proxy_request_buffering off;"
      print "    proxy_read_timeout 3600s;"
      print "    proxy_send_timeout 3600s;"
      print "    send_timeout 3600s;"
      added = 1
    }
    { print }
    END { exit !(in_root && added) }
  ' "$file" >"$temp_file"; then
    rm -f "$temp_file"
    echo "Sub2API 代理片段缺少可定位的根 location。" >&2
    return 1
  fi
  chmod --reference="$file" "$temp_file" 2>/dev/null || true
  mv -- "$temp_file" "$file"
}

add_voice_streaming() {
  file="$1"
  temp_file="$(mktemp "${file}.codex-stream.XXXXXX")"
  if ! awk '
    BEGIN { voice = 0 }
    /^[[:space:]]*location[[:space:]]+\^~[[:space:]]+\/v1\/voice/ { voice = 1 }
    voice && /^[[:space:]]*proxy_buffering[[:space:]]+off[[:space:]]*;/ { buffering = 1 }
    voice && /^[[:space:]]*proxy_request_buffering[[:space:]]+off[[:space:]]*;/ { request_buffering = 1 }
    voice && /^[[:space:]]*proxy_read_timeout[[:space:]]+3600s[[:space:]]*;/ { read_timeout = 1 }
    voice && /^[[:space:]]*proxy_send_timeout[[:space:]]+3600s[[:space:]]*;/ { send_timeout = 1 }
    voice && /^[[:space:]]*send_timeout[[:space:]]+3600s[[:space:]]*;/ { send = 1 }
    voice && /^[[:space:]]*}$/ {
      if (!buffering) print "        proxy_buffering off;"
      if (!request_buffering) print "        proxy_request_buffering off;"
      if (!read_timeout) print "        proxy_read_timeout 3600s;"
      if (!send_timeout) print "        proxy_send_timeout 3600s;"
      if (!send) print "        send_timeout 3600s;"
      print
      voice = 0
      buffering = request_buffering = read_timeout = send_timeout = send = 0
      next
    }
    { print }
  ' "$file" >"$temp_file"; then
    rm -f "$temp_file"
    return 1
  fi
  chmod --reference="$file" "$temp_file" 2>/dev/null || true
  mv -- "$temp_file" "$file"
}

if [ "$sub_needs_fix" = true ]; then
  backup_file "$sub_root"
  add_sub2api_streaming "$sub_root" || rollback
fi
if [ "$voice_needs_fix" = true ]; then
  backup_file "$voice_site"
  add_voice_streaming "$voice_site" || rollback
  if ! grep -Fq 'proxy_set_header Connection "upgrade";' "$voice_site"; then
    sync_parent_directive \
      "$voice_site" \
      '    proxy_set_header Connection "upgrade";' || rollback
  fi
fi

openresty_container="$(
  docker ps --format '{{.Names}}' | grep -E '^1Panel-openresty-' | head -n 1
)"
if [ -z "$openresty_container" ]; then
  rollback
fi
if ! docker exec "$openresty_container" openresty -t; then
  rollback
fi
docker exec "$openresty_container" openresty -s reload

echo "已修复流式代理配置并通过 nginx -t；OpenResty 已重载。备份: $backup_dir"
