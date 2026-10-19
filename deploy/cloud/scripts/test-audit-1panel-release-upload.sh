#!/usr/bin/env bash
# Offline contract checks for audit-1panel-release-upload.sh.
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
audit_script="$script_dir/audit-1panel-release-upload.sh"

bash -n "$audit_script"

fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT

proxy_dir="$fixture_root/www/sites/api.example.test/proxy"
mkdir -p "$proxy_dir"
upload_file="$proxy_dir/release-upload.conf"

cat >"$upload_file" <<'EOF'
location ^~ /api/v1/release-publication/ {
    client_max_body_size 512m;
    client_body_timeout 600s;
    proxy_http_version 1.1;
    proxy_request_buffering off;
    proxy_read_timeout 600s;
    proxy_send_timeout 600s;
    send_timeout 600s;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_pass http://127.0.0.1:8081;
}
EOF

SPROUT_1PANEL_WWW_DIR="$fixture_root/www" \
SPROUT_API_SITE_NAME=api.example.test \
  bash "$audit_script"

sed -i '/proxy_request_buffering off;/d' "$upload_file"
if SPROUT_1PANEL_WWW_DIR="$fixture_root/www" \
  SPROUT_API_SITE_NAME=api.example.test \
  bash "$audit_script" >/dev/null 2>&1; then
  echo "审计错误地接受了会缓冲请求体的上传代理。" >&2
  exit 1
fi

cat >"$upload_file" <<'EOF'
location ^~ /other/ {
    client_max_body_size 512m;
    proxy_request_buffering off;
    proxy_pass http://127.0.0.1:8081;
}
EOF
if SPROUT_1PANEL_WWW_DIR="$fixture_root/www" \
  SPROUT_API_SITE_NAME=api.example.test \
  bash "$audit_script" >/dev/null 2>&1; then
  echo "审计错误地接受了不覆盖发布前缀的代理片段。" >&2
  exit 1
fi

echo "audit-1panel-release-upload shell contract passed."
