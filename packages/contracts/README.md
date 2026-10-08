# Contracts

This directory owns cross-application and cross-service contracts. Keep
transport-specific implementation details inside the owning application or
service.

## Contents

- `openapi/` contains public HTTP API schemas.
- `events/` contains asynchronous event schemas.
- `mqtt/` contains device topic and payload contracts.
- `capabilities/` contains optional hardware capability identifiers.

`schemas/parent_policy.schema.json` describes one child's guardian-editable
policy. `schemas/parent_policy_effective.schema.json` describes the single
most-restrictive policy a family-bound device executes after the platform
aggregates every child policy. Device clients must consume the effective
contract and must not choose a child identifier.

`schemas/device_usage_upload.schema.json` describes the counters and durations
a device uploads for one device-local usage day; uploads are idempotent per
device and date. `schemas/usage_report.schema.json` describes one aggregated
usage day returned to a guardian or support operator. Both contracts carry only
counters, durations, categories, and device metadata. They must never contain
conversation text, audio, images, tokens, or child identifiers.

## Rules

- Use `schema_version` on every breaking contract change.
- Keep identifiers `lower_snake_case`.
- Keep JSON field names `snake_case`.
- Do not place secrets, provider credentials, or environment-specific URLs in
  this directory.
- A contract can be removed when its consumers and generated clients are
  removed.
