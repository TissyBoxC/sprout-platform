# alarm_reminder

`alarm_reminder` owns the guardian alarm schedule (定时提醒、闹钟和日程) and the
bounded per-alarm firing acknowledgement devices report. It ships alongside the
firmware `habit_reminder` cache and the platform `long_term_memory` module: the
guardian edits alarms in the parent app, every bound device reads the enabled
schedule over its device session and rings locally from its own tone catalog,
and the device reports the ringing/snoozed/dismissed/missed state back.

## Contract

One alarm per row, every field bounded here and in the database:

- `alarm_time` a 24-hour `HH:MM` local wall-clock time
- `weekdays` an ISO-8601 set (1 = Monday .. 7 = Sunday); an empty set is
  expanded to every day
- `label` 1..48 characters, `prompt` up to 240 characters
- `snooze_minutes` 1..30
- `enabled` boolean

At most `AlarmCapacity` (64) alarms per family. A firing acknowledgement is one
bounded state per alarm and local `YYYY-MM-DD`, so a report retried after a
reconnect is idempotent. The platform stores no audio: the device synthesizes
the tone itself.

## Surfaces

- Guardian CRUD of the family schedule (`GET`/`POST /api/v1/alarm-reminders`,
  `PUT`/`DELETE /api/v1/alarm-reminders/{alarm_id}`).
- Guardian firing history (`GET /api/v1/alarm-reminders/events`).
- Device schedule pull and firing report (`GET
  /api/v1/devices/{device_id}/alarm-reminders`,
  `POST /api/v1/devices/{device_id}/alarm-reminders/{alarm_id}/events`).
