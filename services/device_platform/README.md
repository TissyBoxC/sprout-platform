# Device Platform

Game console business service for 如此萌屋 · 芽系列·初芽.

## Responsibilities

- parent, family, and child accounts
- device registration and binding
- parent policies and content permissions
- effective family policy delivery to a bound device
- content package metadata
- OTA release and upgrade tasks
- telemetry, audit, and notifications

## Run

```text
go run ./cmd/device-platform
```

The initial service exposes:

```text
GET /healthz
GET /readyz
```

## Administrator Bootstrap

The device platform has no default administrator password and does not share
the `sub2api` account. Create the first administrator from the repository root
after the service containers are healthy:

```powershell
.\tools\initialize-admin.ps1 `
  -Email "admin@clarkhub.cn" `
  -DisplayName "本地管理员" `
  -Password "replace-with-a-strong-password"
```

Scan the printed `otpauth://` URI with an authenticator application. The
password alone is not enough: the console requires the TOTP code from that
application on every login. Running the command again only completes an
unfinished TOTP enrollment; it does not expose an existing secret.

## Configuration

Configuration is read from environment variables. See `configs/config.example.yaml`
for the intended deployment shape.
