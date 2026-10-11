# image_privacy

`image_privacy` owns the guardian image-processing and upload policy for a
family, plus the bounded per-device camera pipeline status the guardian console
reads. It is the platform half of the firmware `image_privacy` component: the
two share schema version `1` field for field.

## Contract

One policy per family, every field bounded here and in the database:

- `processing_mode` in `off` / `metadata` / `local_full`
- `upload_scope` in `none` / `thumbnails` / `analysis`
- `retention_days` 0..30
- `redaction_level` in `none` / `standard` / `strict`
- `enabled`, `upload_allowed`, `local_processing_only`,
  `require_child_consent` booleans

The default policy keeps image processing and upload off and keeps processing
local-only. Disabling the feature pins the mode off and clears upload; choosing
local-only processing also clears upload so a stale upload control never
survives the switch. The platform stores only the policy and bounded status
scalars; no image bytes, thumbnails or face templates are persisted.

## Surfaces

- Guardian read/publish of the family policy (`GET`/`PUT
  /api/v1/image-privacy/policy`).
- Guardian device overview and per-device capability (`GET
  /api/v1/image-privacy/devices`,
  `GET /api/v1/devices/{device_id}/image-privacy/capability`).
- Device-session policy read and status report (`GET
  /api/v1/devices/{device_id}/image-privacy/policy`, `POST
  /api/v1/devices/{device_id}/image-privacy/status`).
- Admin fleet statistics (`GET /api/v1/admin/image-privacy/stats`).

## Status semantics

A device that reports `camera_supported = false` is coerced to a fully off
pipeline with no counters and no block reason, so a stale value never reads as
an active upload. An unauthorised upload is coerced to scope `none`, and
`uploaded_count` can never exceed `processed_count`. A device is `up_to_date`
only when its applied policy revision is at least the current family revision.
