#!/usr/bin/env bash
# Offline contract checks for audit-service-image-override.sh.
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
audit_script="$script_dir/audit-service-image-override.sh"

bash -n "$audit_script"

if ! grep -Eq '^describe_overrides\(\)' "$audit_script"; then
  echo "audit-service-image-override.sh 缺少镜像对比逻辑。" >&2
  exit 1
fi

if ! grep -Eq '^switch_to_registry\(\)' "$audit_script"; then
  echo "audit-service-image-override.sh 缺少安全切换逻辑。" >&2
  exit 1
fi

if ! grep -Fq '$12 != "yes"' "$audit_script"; then
  echo "audit-service-image-override.sh 未拒绝包含真实差异的本地镜像。" >&2
  exit 1
fi

if ! grep -Fq 'docker-compose.override.yml' "$audit_script"; then
  echo "audit-service-image-override.sh 未检查 Compose override。" >&2
  exit 1
fi

if ! grep -Fq '.codex-backups' "$audit_script"; then
  echo "audit-service-image-override.sh 切换前未创建备份。" >&2
  exit 1
fi

if grep -Fq 'require_command jq' "$audit_script"; then
  echo "audit-service-image-override.sh 不应依赖宿主机 jq。" >&2
  exit 1
fi

fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT

mkdir -p "$fixture_root/cloud/scripts"
cp "$audit_script" "$fixture_root/cloud/scripts/audit-service-image-override.sh"
printf '%s\n' 'SPROUT_PLATFORM_VERSION=0.22.0' 'SPROUT_SUB2API_VERSION=0.2.19' > "$fixture_root/cloud/.env"
cat > "$fixture_root/cloud/docker-compose.yml" <<'YAML'
services:
  device_platform:
    image: ghcr.io/tissyboxc/sprout-device-platform:${SPROUT_PLATFORM_VERSION}
  postgres:
    image: postgres:16-alpine
YAML
cat > "$fixture_root/cloud/docker-compose.override.yml" <<'YAML'
services:
  device_platform:
    image: sprout-device-platform:0.22.0
  postgres:
    image: local-postgres:16
YAML

override_services="$(
  awk '
    /^[[:space:]]*[A-Za-z0-9_.-]+:[[:space:]]*$/ {
      section = $1
      sub(/:$/, "", section)
      next
    }
    /^[[:space:]]+image:[[:space:]]*/ && section != "" {
      print section
    }
  ' "$fixture_root/cloud/docker-compose.override.yml"
)"
if [ "$override_services" != $'device_platform\npostgres' ]; then
  echo "测试夹具未解析出预期的 override 服务: $override_services" >&2
  exit 1
fi

if (
  source "$audit_script"
  override_file="$fixture_root/cloud/docker-compose.override.yml"
  is_managed_service postgres
); then
  echo "受管服务白名单错误地接受了 postgres。" >&2
  exit 1
fi

echo "audit-service-image-override shell contract passed."
