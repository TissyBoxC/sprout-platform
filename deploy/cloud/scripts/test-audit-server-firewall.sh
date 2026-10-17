#!/usr/bin/env bash
# Offline contract checks for audit-server-firewall.sh.
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
audit_script="$script_dir/audit-server-firewall.sh"

bash -n "$audit_script"

if ! grep -Fq 'Default: deny (incoming)' "$audit_script"; then
  echo "audit-server-firewall.sh 缺少默认入站拒绝检查。" >&2
  exit 1
fi

if ! grep -Fq 'required_ports="22 80 443 8883"' "$audit_script"; then
  echo "audit-server-firewall.sh 缺少必需放行端口列表。" >&2
  exit 1
fi

for port in 1883 2022 8081 8082 8083 8084 8085 58976; do
  if ! grep -Fq "$port" "$audit_script"; then
    echo "audit-server-firewall.sh 缺少禁止端口检查: $port" >&2
    exit 1
  fi
done

fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT
mkdir -p "$fixture_root/bin"

cat > "$fixture_root/bin/ufw" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
cat <<'STATUS'
Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), deny (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW       Anywhere
80/tcp                     ALLOW       Anywhere
443/tcp                    ALLOW       Anywhere
8883/tcp                   ALLOW       Anywhere
STATUS
EOF
chmod +x "$fixture_root/bin/ufw"

PATH="$fixture_root/bin:$PATH" "$audit_script" >/dev/null

cat > "$fixture_root/bin/ufw" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
cat <<'STATUS'
Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), deny (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW       Anywhere
80/tcp                     ALLOW       Anywhere
443/tcp                    ALLOW       Anywhere
8883/tcp                   ALLOW       Anywhere
58976/tcp                  ALLOW       Anywhere
STATUS
EOF
chmod +x "$fixture_root/bin/ufw"

if PATH="$fixture_root/bin:$PATH" "$audit_script" >/dev/null 2>&1; then
  echo "审计错误地接受了公开的 1Panel 管理端口。" >&2
  exit 1
fi

echo "audit-server-firewall shell contract passed."
