-- Device thermal telemetry reported with the periodic heartbeat.
--
-- The temperature is optional: devices without a sensor, or with the sensor
-- feature disabled, report no reading and keep the 'unknown' state. When a
-- reading is present it is stored in whole tenths of a degree to avoid
-- floating-point drift while staying precise enough for the operator alerting
-- thresholds. 'state' is the normalized band the firmware derives so the admin
-- console does not have to re-derive it from raw values.

ALTER TABLE device_runtime_status
    ADD COLUMN IF NOT EXISTS temperature_state TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS temperature_celsius_tenths INTEGER;

ALTER TABLE device_runtime_status
    DROP CONSTRAINT IF EXISTS device_runtime_temperature_state_valid;

ALTER TABLE device_runtime_status
    ADD CONSTRAINT device_runtime_temperature_state_valid
        CHECK (temperature_state IN ('unknown', 'normal', 'warm', 'hot', 'critical'));

ALTER TABLE device_runtime_status
    DROP CONSTRAINT IF EXISTS device_runtime_temperature_value_valid;

ALTER TABLE device_runtime_status
    ADD CONSTRAINT device_runtime_temperature_value_valid
        CHECK (
            temperature_celsius_tenths IS NULL
            OR temperature_celsius_tenths BETWEEN -400 AND 1250
        );
