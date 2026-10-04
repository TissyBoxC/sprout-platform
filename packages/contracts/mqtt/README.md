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

Interaction events are not a separate MQTT topic. They are delivered inside the
authenticated runtime heartbeat as `diagnostics.interaction_events`, together
with boot, failure, and recovery diagnostics.

Device-specific implementation notes remain in
`services/device_platform/internal/contracts`.
