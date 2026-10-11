# learning_visualization

`learning_visualization` owns the guardian visualization policy for the touch
learning surface, plus the bounded per-device visualization status the guardian
and admin consoles read. It is the platform half of the firmware
`learning_visualization` component: the two share schema version `1` field for
field.

## Contract

One policy per family, every field bounded here and in the database:

- `feedback_style` in `neutral` / `encouraging` / `playful` / `celebration`
- `animation_level` in `off` / `subtle` / `full`
- `max_session_minutes` 1..60
- `enabled`, `touch_feedback_enabled`, `reward_effects_enabled`,
  `lesson_linkage_enabled` booleans

The default policy keeps the feature off and the animation level `off`, so a
device never animates a learning surface before a guardian explicitly enables
it. Disabling the feature pins the derived animation level back to `off`. The
platform stores only bounded scalars; no frame buffers, touch traces or captured
media are persisted.

## Surfaces

- Guardian read/publish of the family policy (`GET`/`PUT
  /api/v1/learning-visualization/policy`).
- Guardian device overview and per-device capability (`GET
  /api/v1/learning-visualization/devices`,
  `GET /api/v1/devices/{device_id}/learning-visualization/capability`).
- Device-session status report (`POST
  /api/v1/devices/{device_id}/learning-visualization/status`).
- Admin fleet statistics (`GET
  /api/v1/admin/learning-visualization/stats`).

## Status semantics

A device that reports `visualization_supported = false` is coerced to the idle
state with animation off, no linked lesson and no event counters, so a stale
value never reads as a live animation. A paused or idle surface always reports
animation `off`. A device is `up_to_date` only when its applied policy revision
is at least the current family revision.
