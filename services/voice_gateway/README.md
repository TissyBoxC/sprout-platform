# Voice Gateway

Realtime voice gateway for 如此萌屋 · 芽系列·初芽 products.

## Responsibilities

- authenticated WebSocket audio sessions
- audio framing, buffering, and playback
- speech recognition and synthesis adapters
- text and vision model execution through the platform-owned AI proxy
- child content policy enforcement before any model call
- latency, duration, and usage telemetry

## Content Policy

The gateway enforces the family's effective parent policy on the server side.
It reads the policy directly from the platform database through the same
read connection used for usage recording; there is no second policy service.

For each voice turn the gateway:

1. Resolves the authenticated device's family from `device_bindings`.
2. Reads the most-restrictive aggregate of the family's child policies,
   including the youngest child age tier.
3. Refuses a turn whose declared `content_category` is not in the effective
   allowlist, returning a stable `content_category_not_allowed` reason.
4. Fails closed with `parent_policy_unavailable` when the policy cannot be
   resolved, and `parent_policy_not_found` when the family has no policy.
5. Composes the system prompt from the allowed categories and age tier so the
   model cannot silently widen content.

Older firmware that does not declare a `content_category` still receives the
age-tier-constrained prompt so existing devices keep working. A session start
frame may carry an optional `content_category`; the value is validated as a
lower-case category identifier and never logged.

Crisis and self-harm utterances are exempt from ordinary category refusal and
receive a fixed protective reply without calling the language model. Safety
responses must never be blocked by a family category, a model outage, or a
policy lookup failure. The input and output moderation stages are independent
operator switches, both defaulting to `true`; disabling either cannot disable
the crisis path.

Recognized input and model output are checked for sexual, violent, self-harm,
illegal, horror, privacy, offline-meeting, commercial-inducement,
prompt-injection, secret-exfiltration, and personal-data patterns. Unsafe
model output is replaced with an age-appropriate fallback instead of being
spoken. Errors and logs expose only a stable message and redact credentials,
phone numbers, email addresses, and national identifiers.

Configure this with `VOICE_GATEWAY_DATABASE_DSN`. When content policy is
enabled but no database is configured, the realtime endpoint refuses to start
instead of running without enforcement. Set
`VOICE_GATEWAY_CONTENT_POLICY_ENABLED=false` only for local diagnostics where
no child traffic exists.

## Guardian Intercom Relay

Intercom is a guardian-initiated call that the child must accept on the device
before any audio flows. `device_platform` owns the call record and the accept
step; the gateway only validates the call and relays frames.

Set `VOICE_GATEWAY_INTERCOM_ENABLED=true` to mount the relay. It requires the
internal API to be enabled with a service token of at least 32 characters,
because authorization is an internal endpoint that `device_platform` calls.

1. After the child accepts, `device_platform` calls
   `POST /internal/v1/intercom/sessions/{session_id}/relay` with `device_id`
   and the short-lived `device_token` (Bearer internal service token).
2. The gateway re-reads the call from
   `GET {device_platform}/internal/v1/devices/{device_id}/intercom-calls/{session_id}`
   and refuses anything whose state is not `active`. A `ringing` call never
   opens a relay, so the accept step cannot be bypassed by a caller that merely
   claims the call is active.
3. The guardian connects to `GET /v1/intercom/{session_id}?device_id=...`. That
   route only attaches to an already authorized relay and accepts exactly one
   guardian socket per call.
4. Binary frames are relayed in both directions; text control frames are
   limited to `{"type":"hangup"}` and `{"type":"ping"}`.

Limits: the relay closes after the smaller of
`VOICE_GATEWAY_INTERCOM_MAX_CALL_DURATION_SECONDS` (maximum 600, matching the
platform domain limit) and the platform call deadline. A hangup from either
side, a dead socket, or the cap closes both sides immediately and releases the
relay buffers. No audio is recorded, persisted, or logged; the close record
contains only the session id, device id, duration, and a fixed close reason.

Stable error codes: `intercom_session_not_found`, `intercom_session_not_active`,
`intercom_session_expired`, `intercom_invalid_request`,
`intercom_session_already_open`, `intercom_limit_reached`,
`intercom_relay_invalid`, `intercom_unavailable`.

## Platform AI Execution

The gateway never receives a global Sub2API or upstream provider key. It calls
`device_platform` with the internal service token:

1. `GET /internal/v1/devices/{device_id}/ai-execution-ticket` with `model`,
   `project`, and `session_id`. Voice conversation uses `project=voice`;
   vision uses `project=vision`.
2. `POST /internal/v1/ai-execution?ticket=...` with the unchanged OpenAI chat
   completions body. The platform atomically consumes the ticket, decrypts the
   guardian account's credential, and adds the real upstream `Authorization`
   header.

The gateway only sends `Authorization: Bearer <internal-service-token>` to
`device_platform`. Every retry requests a fresh ticket. A request without a
real device ID and session ID is rejected before any HTTP call.

Configure the internal connection with:

```text
VOICE_GATEWAY_DEVICE_PLATFORM_BASE_URL=http://device_platform:8081
VOICE_GATEWAY_DEVICE_PLATFORM_SERVICE_TOKEN=<SPROUT_INTERNAL_SERVICE_TOKEN>
```

Do not expose this base URL or service token through a public reverse proxy,
browser code, device firmware, or logs. Startup fails closed when vision or a
model-backed realtime pipeline is enabled without a base URL and a service
token of at least 32 characters.

## Errors

Stable policy reasons returned in the `error` control frame include:

- `content_category_not_allowed`
- `content_category_invalid`
- `parent_policy_unavailable`
- `parent_policy_not_found`

These reasons contain no child identifiers, prompt text, or upstream
credentials.

## Run

```text
go run ./cmd/voice-gateway
```

The initial service exposes:

```text
GET /healthz
GET /readyz
```

## Configuration

Configuration is read from environment variables. See
`configs/config.example.yaml` for the intended deployment shape. The policy
path requires `VOICE_GATEWAY_DATABASE_DSN`; the realtime WebSocket path
requires `VOICE_GATEWAY_WS_ENABLED=true` and a strong
`VOICE_GATEWAY_WS_TOKEN_SECRET`.

Browser WebSocket clients must list their exact origin in
`VOICE_GATEWAY_WS_ALLOWED_ORIGINS` (comma-separated). Device firmware and
native clients that do not send an `Origin` header remain supported because
they authenticate with a device token before the upgrade. Any browser request
with an unlisted origin is rejected.

## Device Token Revocation

The gateway rejects device tokens with a missing or blank `token_id`. When a
device connects, the verifier atomically consumes the signed `token_id` in
Redis. The token cannot be used for a second connection by any replica, even
when two gateways receive the same token concurrently. When the connection
closes, the shared record is extended through the signed expiry plus the
verifier leeway; Redis expires the record automatically, keeping the store
bounded.

Redis errors fail authentication closed. The process-local claim map is only a
fallback for unit tests and single-process tooling; production wiring always
uses the shared Redis store. Do not run multiple gateway replicas without a
reachable shared claim store.
