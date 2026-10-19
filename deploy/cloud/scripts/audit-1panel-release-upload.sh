#!/usr/bin/env bash
# Read-only audit for the API site's large release-upload proxy location.
#
# The normal 1Panel API proxy uses the default request-body limit and buffers
# request bodies. Release artifacts are much larger, so the dedicated upload
# location must stream requests, allow large bodies, and keep long timeouts.
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
default_www_dir="/opt/1panel/www"
www_dir="${SPROUT_1PANEL_WWW_DIR:-$default_www_dir}"
site_name="${SPROUT_API_SITE_NAME:-api.clarkhub.cn}"
upload_file="$www_dir/sites/$site_name/proxy/release-upload.conf"

usage() {
  cat >&2 <<'EOF'
用法:
  audit-1panel-release-upload.sh

只读检查 1Panel 的 API 站点是否为
/api/v1/release-publication/ 配置了大请求体、流式上传和长超时。
不会修改站点配置，也不会探测或上传真实数据。
EOF
}

if [ "${1:-}" = "--help" ] || [ "${1:-}" = "-h" ]; then
  usage
  exit 0
fi
if [ "$#" -ne 0 ]; then
  usage
  exit 1
fi

if [ ! -f "$upload_file" ]; then
  echo "缺少发布上传代理片段: $upload_file" >&2
  exit 1
fi

awk '
  /^[[:space:]]*location[[:space:]]+\^~[[:space:]]+\/api\/v1\/release-publication\// {
    upload = 1
  }
  upload && /^[[:space:]]*client_max_body_size[[:space:]]+512m[[:space:]]*;/ {
    body_size = 1
  }
  upload && /^[[:space:]]*client_body_timeout[[:space:]]+600s[[:space:]]*;/ {
    body_timeout = 1
  }
  upload && /^[[:space:]]*proxy_http_version[[:space:]]+1\.1[[:space:]]*;/ {
    http_version = 1
  }
  upload && /^[[:space:]]*proxy_request_buffering[[:space:]]+off[[:space:]]*;/ {
    request_buffering = 1
  }
  upload && /^[[:space:]]*proxy_read_timeout[[:space:]]+600s[[:space:]]*;/ {
    read_timeout = 1
  }
  upload && /^[[:space:]]*proxy_send_timeout[[:space:]]+600s[[:space:]]*;/ {
    send_timeout = 1
  }
  upload && /^[[:space:]]*send_timeout[[:space:]]+600s[[:space:]]*;/ {
    socket_timeout = 1
  }
  upload && /^[[:space:]]*proxy_pass[[:space:]]+http:\/\/127\.0\.0\.1:8081[[:space:]]*;/ {
    upstream = 1
  }
  END {
    exit !(upload && body_size && body_timeout && http_version &&
      request_buffering && read_timeout && send_timeout && socket_timeout &&
      upstream)
  }
' "$upload_file"

printf 'release_upload_proxy=ok\n'
printf 'config=%s\n' "$upload_file"
