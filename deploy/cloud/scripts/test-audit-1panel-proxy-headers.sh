#!/usr/bin/env bash
# Offline contract checks for audit-1panel-proxy-headers.sh.
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
audit_script="$script_dir/audit-1panel-proxy-headers.sh"

bash -n "$audit_script"

if ! grep -Fq 'add_header X-Cache' "$audit_script"; then
  echo "audit-1panel-proxy-headers.sh 缺少 X-Cache 检查。" >&2
  exit 1
fi

if ! grep -Fq 'Strict-Transport-Security' "$audit_script"; then
  echo "audit-1panel-proxy-headers.sh 缺少 HSTS 修复。" >&2
  exit 1
fi

if ! grep -Fq 'openresty -t' "$audit_script"; then
  echo "audit-1panel-proxy-headers.sh 修改后未校验 OpenResty 配置。" >&2
  exit 1
fi

if ! grep -Fq 'rollback' "$audit_script"; then
  echo "audit-1panel-proxy-headers.sh 缺少校验失败回滚。" >&2
  exit 1
fi

fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT

www_dir="$fixture_root/www"
proxy_dir="$www_dir/sites/api.example.test/proxy"
mkdir -p "$proxy_dir" "$fixture_root/bin"

cat > "$proxy_dir/root.conf" <<'EOF'
location ^~ / {
    proxy_pass http://127.0.0.1:8081;
    add_header X-Cache $upstream_cache_status;
}
EOF

cat > "$fixture_root/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${CODEX_DOCKER_LOG:?}"
if [ "$1" = "ps" ]; then
  printf '%s\n' '1Panel-openresty-codex'
fi
EOF
chmod +x "$fixture_root/bin/docker"

if SPROUT_1PANEL_WWW_DIR="$www_dir" "$audit_script" >/dev/null 2>&1; then
  echo "只读审计错误地接受了缺少 HSTS 的代理片段。" >&2
  exit 1
fi

SPROUT_1PANEL_WWW_DIR="$www_dir" \
  PATH="$fixture_root/bin:$PATH" \
  CODEX_DOCKER_LOG="$fixture_root/docker.log" \
  "$audit_script" --apply

grep -Fq 'add_header X-Cache $upstream_cache_status always;' "$proxy_dir/root.conf"
grep -Fq 'add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;' "$proxy_dir/root.conf"

if [ "$(grep -Fc 'add_header Strict-Transport-Security' "$proxy_dir/root.conf")" -ne 1 ]; then
  echo "代理片段应恰好包含一条 HSTS 响应头。" >&2
  exit 1
fi

grep -Fq 'exec 1Panel-openresty-codex openresty -t' "$fixture_root/docker.log"
grep -Fq 'exec 1Panel-openresty-codex openresty -s reload' "$fixture_root/docker.log"

if ! SPROUT_1PANEL_WWW_DIR="$www_dir" "$audit_script" >/dev/null 2>&1; then
  echo "修复后的代理片段未通过只读审计。" >&2
  exit 1
fi

echo "audit-1panel-proxy-headers shell contract passed."
