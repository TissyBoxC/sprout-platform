# video_session

`video_session` owns the guardian media policy for the device video call and
remote-companionship session, plus the bounded per-device session status the
guardian console reads. It is the platform half of the firmware `video_session`
component: the two share schema version `1` field for field.

## Contract

One policy per family, every field bounded here and in the database:

- `ring_timeout_seconds` 5..120
- `max_session_seconds` 60..7200
- `quality_floor` in `unknown` / `good` / `fair` / `poor`
- `enabled`, `camera_allowed`, `microphone_allowed`, `require_child_consent`
  booleans

The default policy keeps the camera and microphone off, so a device never starts
capturing before a guardian explicitly enables a permission. The platform stores
only negotiation metadata; no audio or video media is persisted.

## Surfaces

- Guardian read/publish of the family policy (`GET`/`PUT
  /api/v1/video-session/policy`).
- Guardian device overview and per-device capability (`GET
  /api/v1/video-session/devices`,
  `GET /api/v1/devices/{device_id}/video-session/capability`).
- Device-session status report (`POST
  /api/v1/devices/{device_id}/video-session/status`).
- Admin fleet statistics (`GET /api/v1/admin/video-session/stats`).

## Status semantics

A device that reports `session_supported = false` is coerced to the idle state
with no permissions and no end reason, so a stale value never reads as a live
capture. A device is `up_to_date` only when its applied policy revision is at
least the current family revision.
