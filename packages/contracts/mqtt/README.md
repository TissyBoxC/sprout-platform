# MQTT Contracts

The `1.0.0` device runtime channel uses these versioned topics:

| Topic | Direction | QoS | Retain | Payload |
| --- | --- | --- | --- | --- |
| `sprout/v1/devices/{device_id}/runtime` | device to platform | 1 | false | `device_runtime.schema.json` |
| `sprout/v1/devices/{device_id}/commands` | platform to device | 1 | false | `device_event.schema.json` |
| `sprout/v1/devices/{device_id}/commands/ack` | device to platform | 1 | false | `device_event.schema.json` |

The runtime publisher sets `heartbeat_id` to a unique, monotonically changing
value for every measurement. The platform treats the pair
`(device_id, heartbeat_id)` as the idempotency key and accepts a retransmitted
QoS 1 message without updating the status twice.

Command messages carry `request_id` and `event_id`. The device acknowledges
each command with the same `request_id`; the platform rejects a repeated
acknowledgement and does not execute the command again.

## Implementation status

- `device_platform` publishes `device.command` events to
  `sprout/v1/devices/{device_id}/commands` with QoS 1 and retain=false when an
  operator command is created. Publishing is best-effort: the durable command
  row and the device's authenticated `GET /api/v1/devices/{device_id}/runtime/commands`
  poll remain the recovery path, so a broker outage never loses a command.
- The device-side MQTT subscriber is not implemented yet. Firmware currently
  consumes commands through the authenticated HTTP poll only; both transports
  are specified to be idempotent on `request_id`.

Interaction events are not a separate MQTT topic. They are delivered inside the
authenticated runtime heartbeat as `diagnostics.interaction_events`, together
with boot, failure, and recovery diagnostics.

Device-specific implementation notes remain in
`services/device_platform/internal/contracts`.
