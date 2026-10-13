#!/usr/bin/env bash
# Audit the Sprout host firewall without changing the active rules.
#
# The public surface is intentionally small:
#   22/tcp   SSH
#   80/tcp   HTTP -> HTTPS redirect
#   443/tcp  public HTTPS
#   8883/tcp MQTT over TLS
#
# Everything else, including the 1Panel management port, must not be reachable
# from the public interface. This script is read-only so it is safe to run on a
# live host; remediation remains an explicit operator action.
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
用法:
  audit-server-firewall.sh

作用:
  只读检查 UFW 是否启用、默认入站策略是否为拒绝，以及公网入口是否只放行
  22、80、443、8883。脚本不会修改防火墙。
EOF
}

if [ "${1:-}" = "--help" ] || [ "${1:-}" = "-h" ]; then
  usage
  exit 0
fi

if [ "$#" -gt 0 ]; then
  usage
  exit 1
fi

if ! command -v ufw >/dev/null 2>&1; then
  echo "缺少 ufw 命令。" >&2
  exit 1
fi

status="$(ufw status verbose 2>/dev/null || true)"
if ! printf '%s\n' "$status" | grep -Fq 'Status: active'; then
  echo "UFW 未启用。" >&2
  exit 1
fi

if ! printf '%s\n' "$status" | grep -Fq 'Default: deny (incoming)'; then
  echo "UFW 默认入站策略不是 deny。" >&2
  exit 1
fi

required_ports="22 80 443 8883"
for port in $required_ports; do
  if ! printf '%s\n' "$status" |
    grep -Eq "^${port}/tcp[[:space:]]+ALLOW IN[[:space:]]+Anywhere"; then
    echo "UFW 未放行必需的 TCP 端口: $port" >&2
    exit 1
  fi
done

forbidden_ports="1883 2022 8081 8082 8083 8084 8085 58976"
for port in $forbidden_ports; do
  if printf '%s\n' "$status" |
    grep -Eq "^${port}/tcp[[:space:]]+ALLOW IN[[:space:]]+Anywhere"; then
    echo "UFW 不应向公网放行端口: $port" >&2
    exit 1
  fi
done

echo "UFW 公网入口规则正确: 22, 80, 443, 8883。"
