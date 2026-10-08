# Voice Gateway

Realtime voice gateway for 如此萌屋 · 芽系列·初芽 products.

## Responsibilities

- authenticated WebSocket audio sessions
- audio framing, buffering, and playback
- speech recognition and synthesis adapters
- text conversation through `sub2api`
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

## Device Token Revocation

The gateway rejects device tokens with a missing or blank `token_id`. When a
WebSocket connection closes, the verifier revokes that token identifier in
process memory until its signed expiry, closing the replay window for a closed
conversation.

This revocation set is **process-local**. The current deployment topology is a
single gateway replica, so the guarantee holds there. Before running multiple
gateway replicas, move the revocation set to shared state such as Redis or the
platform database; otherwise a token revoked by one replica can still be
accepted by another.
