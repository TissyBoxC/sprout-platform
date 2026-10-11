# bluetooth_audio

`bluetooth_audio` owns the guardian Bluetooth audio policy for the device
speaker mode and pairing, plus the bounded per-device adapter status the
guardian console reads. It is the platform half of the firmware
`bluetooth_audio` component: the two share schema version `1` field for field.

## Contract

One policy per family, every field bounded here and in the database:

- `preferred_codec` in `auto` / `sbc` / `aac` / `ldac`
- `max_volume_percent` 0..100
- `display_name` at most 48 characters
- `enabled`, `speaker_mode_allowed`, `pairing_allowed`, `auto_reconnect`
  booleans

The default policy keeps Bluetooth audio and pairing off, so a device never
advertises or accepts a pairing before a guardian explicitly enables it.
Disabling the feature clears the speaker and pairing flags so a stale control
never reads as live. The platform stores only the policy and bounded status
scalars; no audio, pairing keys or peer addresses are persisted.

## Surfaces

- Guardian read/publish of the family policy (`GET`/`PUT
  /api/v1/bluetooth-audio/policy`).
- Guardian device overview and per-device capability (`GET
  /api/v1/bluetooth-audio/devices`,
  `GET /api/v1/devices/{device_id}/bluetooth-audio/capability`).
- Device-session policy read and status report (`GET
  /api/v1/devices/{device_id}/bluetooth-audio/policy`, `POST
  /api/v1/devices/{device_id}/bluetooth-audio/status`).
- Admin fleet statistics (`GET /api/v1/admin/bluetooth-audio/stats`).

## Status semantics

A device that reports `bluetooth_supported = false` is coerced to a fully off
adapter with no pairings, no connected devices, no volume and no disconnect
reason, so a stale value never reads as an active speaker. Speaker mode is also
cleared unless the adapter is actually connected. A device is `up_to_date` only
when its applied policy revision is at least the current family revision.
