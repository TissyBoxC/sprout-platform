# HTTP Contracts

Versioned HTTP schemas exposed by Sprout services live here.

## Device runtime

`device_runtime.yaml` covers:

- authenticated device heartbeat submission;
- device-side command polling and acknowledgement;
- effective parent-policy delivery for a bound device;
- guardian runtime status for owned devices;
- operations-console device listing and maintenance commands.

Runtime payload fields are defined by `schemas/device_runtime.schema.json`.
The MQTT equivalent of the same state contract is documented in `mqtt/`.

## Conventions

- Public endpoints use the shared success envelope from `common.schema.json`.
- Error responses use `errors/error_response.schema.json`.
- Device identifiers follow the canonical snake-case identifier rules.
- Timestamps are UTC RFC 3339 values.
- Client-visible errors never include internal identifiers or stack traces.
