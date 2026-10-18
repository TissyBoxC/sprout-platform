#!/usr/bin/env bash
# Reject incomplete or malformed cloud values before Compose starts containers.
set -euo pipefail

cloud_dir="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
env_file="${1:-$cloud_dir/.env}"

if [ ! -f "$env_file" ]; then
  echo "Cloud environment file not found: $env_file" >&2
  echo "Copy .env.example to .env and configure every required value first." >&2
  exit 1
fi

read_value() {
  key="$1"
  awk -F= -v key="$key" '
    $1 == key {
      sub(/^[^=]*=/, "")
      sub(/\r$/, "")
      print
      exit
    }
  ' "$env_file"
}

is_hex_64() {
  value="$1"
  [ "${#value}" -eq 64 ] && printf '%s' "$value" | grep -Eq '^[0-9a-fA-F]{64}$'
}

validation_failed=0

require_value() {
  key="$1"
  value="$(read_value "$key")"
  if [ -z "$value" ]; then
    echo "Invalid cloud environment: $key is missing or empty." >&2
    validation_failed=1
    return
  fi
  case "$value" in
    replace-with-*|change-me*|your-*)
      echo "Invalid cloud environment: $key still contains an example value." >&2
      validation_failed=1
      ;;
  esac
}

required_keys="
SPROUT_POSTGRES_PASSWORD
SPROUT_REDIS_PASSWORD
SPROUT_INTERNAL_SERVICE_TOKEN
SPROUT_RELEASE_UPLOAD_TOKEN
SPROUT_AUTH_ACCESS_TOKEN_SECRET
SPROUT_AI_CREDENTIAL_KEY
SPROUT_MFA_CREDENTIAL_KEY
SPROUT_DEVICE_PLATFORM_UID
SPROUT_DEVICE_PLATFORM_GID
SPROUT_SERVICE_VERSION_STATE_UID
SPROUT_SERVICE_VERSION_STATE_GID
SPROUT_SUB2API_ADMIN_EMAIL
SPROUT_SUB2API_ADMIN_PASSWORD
SPROUT_SUB2API_JWT_SECRET
SPROUT_SUB2API_TOTP_ENCRYPTION_KEY
SPROUT_DOWNLOAD_SFTP_PORT
SPROUT_DOWNLOAD_HTTP_PORT
SPROUT_DOWNLOAD_SFTP_USER
SPROUT_DOWNLOAD_SFTP_GID
SPROUT_DOWNLOAD_PUBLIC_BASE_URL
"

for key in $required_keys; do
  require_value "$key"
done

require_http_url() {
  key="$1"
  value="$2"
  scheme="$3"
  case "$value" in
    "$scheme"://?*)
      ;;
    *)
      echo "Invalid cloud environment: $key must be an absolute $scheme URL." >&2
      validation_failed=1
      ;;
  esac
}

require_numeric_id() {
  key="$1"
  value="$(read_value "$key")"
  if [ -n "$value" ] && ! printf '%s' "$value" | grep -Eq '^[0-9]+$'; then
    echo "Invalid cloud environment: $key must be a numeric container UID or GID." >&2
    validation_failed=1
  fi
}

for key in \
  SPROUT_DEVICE_PLATFORM_UID \
  SPROUT_DEVICE_PLATFORM_GID \
  SPROUT_SERVICE_VERSION_STATE_UID \
  SPROUT_SERVICE_VERSION_STATE_GID; do
  require_numeric_id "$key"
done

platform_uid="$(read_value SPROUT_DEVICE_PLATFORM_UID)"
platform_gid="$(read_value SPROUT_DEVICE_PLATFORM_GID)"
state_uid="$(read_value SPROUT_SERVICE_VERSION_STATE_UID)"
state_gid="$(read_value SPROUT_SERVICE_VERSION_STATE_GID)"

if [ -n "$platform_uid" ] && [ -n "$state_uid" ] && [ "$platform_uid" != "$state_uid" ]; then
  echo "Invalid cloud environment: SPROUT_SERVICE_VERSION_STATE_UID must match SPROUT_DEVICE_PLATFORM_UID so the console can write upgrade requests." >&2
  validation_failed=1
fi

if [ -n "$platform_gid" ] && [ -n "$state_gid" ] && [ "$platform_gid" != "$state_gid" ]; then
  echo "Invalid cloud environment: SPROUT_SERVICE_VERSION_STATE_GID must match SPROUT_DEVICE_PLATFORM_GID so the console and worker share upgrade volume access." >&2
  validation_failed=1
fi

totp_key="$(read_value SPROUT_SUB2API_TOTP_ENCRYPTION_KEY)"
if [ -n "$totp_key" ] && ! is_hex_64 "$totp_key"; then
  echo "Invalid cloud environment: SPROUT_SUB2API_TOTP_ENCRYPTION_KEY must be exactly 64 hexadecimal characters." >&2
  echo "Generate one with: openssl rand -hex 32" >&2
  validation_failed=1
fi

admin_email="$(read_value SPROUT_SUB2API_ADMIN_EMAIL)"
if [ -n "$admin_email" ] && ! printf '%s' "$admin_email" | grep -Eq '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$'; then
  echo "Invalid cloud environment: SPROUT_SUB2API_ADMIN_EMAIL must be a valid email address." >&2
  validation_failed=1
fi

case "$(printf '%s' "$(read_value SPROUT_VOICE_GATEWAY_WS_ENABLED)" | tr '[:upper:]' '[:lower:]')" in
  false|0|"")
    ;;
  true|1)
    for key in \
      SPROUT_VOICE_GATEWAY_PUBLIC_WS_URL \
      SPROUT_VOICE_GATEWAY_WS_TOKEN_SECRET \
      SPROUT_VOICE_GATEWAY_ASR_PROVIDER \
      SPROUT_VOICE_GATEWAY_TTS_PROVIDER \
      SPROUT_VOICE_GATEWAY_LLM_MODEL; do
      require_value "$key"
    done

    public_ws_url="$(read_value SPROUT_VOICE_GATEWAY_PUBLIC_WS_URL)"
    if [ -n "$public_ws_url" ]; then
      require_http_url SPROUT_VOICE_GATEWAY_PUBLIC_WS_URL "$public_ws_url" wss
    fi

    ws_token_secret="$(read_value SPROUT_VOICE_GATEWAY_WS_TOKEN_SECRET)"
    if [ -n "$ws_token_secret" ] && [ "${#ws_token_secret}" -lt 32 ]; then
      echo "Invalid cloud environment: SPROUT_VOICE_GATEWAY_WS_TOKEN_SECRET must contain at least 32 characters." >&2
      validation_failed=1
    fi

    continuity_window="$(read_value SPROUT_VOICE_GATEWAY_WS_CONTINUITY_WINDOW_SECONDS)"
    if [ -z "$continuity_window" ]; then
      echo "Invalid cloud environment: SPROUT_VOICE_GATEWAY_WS_CONTINUITY_WINDOW_SECONDS is missing or empty." >&2
      validation_failed=1
    elif ! printf '%s' "$continuity_window" | grep -Eq '^[0-9]{1,3}$'; then
      echo "Invalid cloud environment: SPROUT_VOICE_GATEWAY_WS_CONTINUITY_WINDOW_SECONDS must be numeric." >&2
      validation_failed=1
    elif [ "$continuity_window" -lt 5 ] || [ "$continuity_window" -gt 600 ]; then
      echo "Invalid cloud environment: SPROUT_VOICE_GATEWAY_WS_CONTINUITY_WINDOW_SECONDS must be between 5 and 600." >&2
      validation_failed=1
    fi
    ;;
  *)
    echo "Invalid cloud environment: SPROUT_VOICE_GATEWAY_WS_ENABLED must be true or false." >&2
    validation_failed=1
    ;;
esac

if [ "$validation_failed" -ne 0 ]; then
  exit 1
fi

echo "Cloud environment validation passed: $env_file"
