#!/usr/bin/env bash
# Offline contract checks for audit-1panel-streaming-proxy.sh.
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
audit_script="$script_dir/audit-1panel-streaming-proxy.sh"

bash -n "$audit_script"

for required in \
  'proxy_buffering off' \
  'proxy_request_buffering off' \
  'proxy_read_timeout 3600s' \
  'proxy_send_timeout 3600s' \
  'send_timeout 3600s' \
  'openresty -t' \
  'rollback'
do
  if ! grep -Fq "$required" "$audit_script"; then
    echo "audit-1panel-streaming-proxy.sh 缺少契约: $required" >&2
    exit 1
  fi
done

fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT

www_dir="$fixture_root/www"
sub_dir="$www_dir/sites/sub.clarkhub.cn/proxy"
voice_dir="$www_dir/sites/voice.clarkhub.cn/proxy"
mkdir -p "$sub_dir" "$voice_dir" "$www_dir/conf.d" "$fixture_root/bin"

cat >"$sub_dir/root.conf" <<'EOF'
location ^~ / {
    proxy_pass http://127.0.0.1:8084;
    proxy_http_version 1.1;
    add_header X-Test "not the only marker" always;
}

location ^~ /api/ {
    proxy_pass http://127.0.0.1:8084;
}
EOF

cat >"$voice_dir/root.conf" <<'EOF'
location ^~ / {
    proxy_pass http://127.0.0.1:8082;
}
EOF

cat >"$www_dir/conf.d/voice.clarkhub.cn.conf" <<'EOF'
server {
    server_name voice.clarkhub.cn;
    location ^~ /v1/voice {
        proxy_pass http://127.0.0.1:8082;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
    }
}
EOF

cat >"$fixture_root/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${CODEX_DOCKER_LOG:?}"
if [ "$1" = "ps" ]; then
  printf '%s\n' '1Panel-openresty-codex'
fi
EOF
chmod +x "$fixture_root/bin/docker"

if SPROUT_1PANEL_WWW_DIR="$www_dir" \
  "$audit_script" >/dev/null 2>&1; then
  echo "只读审计错误地接受了缺少流式参数的代理配置。" >&2
  exit 1
fi

SPROUT_1PANEL_WWW_DIR="$www_dir" \
  PATH="$fixture_root/bin:$PATH" \
  CODEX_DOCKER_LOG="$fixture_root/docker.log" \
  "$audit_script" --apply

grep -Fq 'proxy_buffering off;' "$sub_dir/root.conf"
grep -Fq 'proxy_request_buffering off;' "$sub_dir/root.conf"
grep -Fq 'proxy_read_timeout 3600s;' "$sub_dir/root.conf"
grep -Fq 'proxy_send_timeout 3600s;' "$sub_dir/root.conf"
grep -Fq 'send_timeout 3600s;' "$sub_dir/root.conf"
root_end_line="$(grep -n '^}' "$sub_dir/root.conf" | head -n 1 | cut -d: -f1)"
for directive in \
  'proxy_buffering off;' \
  'proxy_request_buffering off;' \
  'proxy_read_timeout 3600s;' \
  'proxy_send_timeout 3600s;' \
  'send_timeout 3600s;'
do
  line="$(grep -nF "$directive" "$sub_dir/root.conf" | head -n 1 | cut -d: -f1)"
  if [ -z "$line" ] || [ "$line" -ge "$root_end_line" ]; then
    echo "Sub2API 流式参数写到了根 location 之外: $directive" >&2
    exit 1
  fi
done

grep -Fq 'proxy_buffering off;' "$www_dir/conf.d/voice.clarkhub.cn.conf"
grep -Fq 'proxy_request_buffering off;' "$www_dir/conf.d/voice.clarkhub.cn.conf"
grep -Fq 'proxy_read_timeout 3600s;' "$www_dir/conf.d/voice.clarkhub.cn.conf"
grep -Fq 'proxy_send_timeout 3600s;' "$www_dir/conf.d/voice.clarkhub.cn.conf"
grep -Fq 'send_timeout 3600s;' "$www_dir/conf.d/voice.clarkhub.cn.conf"
grep -Fq 'proxy_set_header Connection "upgrade";' "$www_dir/conf.d/voice.clarkhub.cn.conf"

grep -Fq 'exec 1Panel-openresty-codex openresty -t' "$fixture_root/docker.log"
grep -Fq 'exec 1Panel-openresty-codex openresty -s reload' "$fixture_root/docker.log"

if ! SPROUT_1PANEL_WWW_DIR="$www_dir" \
  "$audit_script" >/dev/null 2>&1; then
  echo "修复后的流式代理配置未通过只读审计。" >&2
  exit 1
fi

echo "audit-1panel-streaming-proxy shell contract passed."
