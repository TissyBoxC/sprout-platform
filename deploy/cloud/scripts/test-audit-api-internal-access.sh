#!/usr/bin/env bash
# Offline contract checks for audit-api-internal-access.sh.
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
audit_script="$script_dir/audit-api-internal-access.sh"

bash -n "$audit_script"

if ! grep -Fq 'location ^~ /internal' "$audit_script"; then
  echo "audit-api-internal-access.sh 缺少 /internal location 检查。" >&2
  exit 1
fi

if ! grep -Fq 'return 404;' "$audit_script"; then
  echo "audit-api-internal-access.sh 缺少 404 拒绝规则。" >&2
  exit 1
fi

if ! grep -Fq 'openresty -t' "$audit_script"; then
  echo "audit-api-internal-access.sh 修改后未校验 OpenResty 配置。" >&2
  exit 1
fi

if ! grep -Fq 'rollback' "$audit_script"; then
  echo "audit-api-internal-access.sh 缺少校验失败回滚。" >&2
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

if SPROUT_1PANEL_WWW_DIR="$www_dir" \
  SPROUT_API_SITE_NAME="api.example.test" \
  "$audit_script" >/dev/null 2>&1; then
  echo "只读审计错误地接受了未拦截 /internal 的 API 站点。" >&2
  exit 1
fi

SPROUT_1PANEL_WWW_DIR="$www_dir" \
  SPROUT_API_SITE_NAME="api.example.test" \
  PATH="$fixture_root/bin:$PATH" \
  CODEX_DOCKER_LOG="$fixture_root/docker.log" \
  "$audit_script" --apply

test -f "$proxy_dir/internal-access.conf"
grep -Fq 'location ^~ /internal' "$proxy_dir/internal-access.conf"
grep -Fq 'return 404;' "$proxy_dir/internal-access.conf"

if [ "$(grep -Fc 'location ^~ /internal' "$proxy_dir/internal-access.conf")" -ne 1 ]; then
  echo "API 站点应恰好包含一条 /internal 拦截规则。" >&2
  exit 1
fi

grep -Fq 'exec 1Panel-openresty-codex openresty -t' "$fixture_root/docker.log"
grep -Fq 'exec 1Panel-openresty-codex openresty -s reload' "$fixture_root/docker.log"

if ! SPROUT_1PANEL_WWW_DIR="$www_dir" \
  SPROUT_API_SITE_NAME="api.example.test" \
  "$audit_script" >/dev/null 2>&1; then
  echo "修复后的 API 站点未通过只读审计。" >&2
  exit 1
fi

docker_calls_before="$(wc -l < "$fixture_root/docker.log")"
SPROUT_1PANEL_WWW_DIR="$www_dir" \
  SPROUT_API_SITE_NAME="api.example.test" \
  PATH="$fixture_root/bin:$PATH" \
  CODEX_DOCKER_LOG="$fixture_root/docker.log" \
  "$audit_script" --apply >/dev/null

if [ "$(wc -l < "$fixture_root/docker.log")" -ne "$docker_calls_before" ]; then
  echo "幂等应用不应再次 reload OpenResty。" >&2
  exit 1
fi

echo "audit-api-internal-access shell contract passed."
