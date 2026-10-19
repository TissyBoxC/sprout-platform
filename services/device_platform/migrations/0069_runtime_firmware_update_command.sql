-- Runtime command vocabulary includes every command already handled by the
-- runtime service. Firmware updates are delivery triggers for an OTA release
-- that was already authorized through the release/deployment workflow; the
-- payload is validated against the OTA module before this row can be written.

ALTER TABLE device_runtime_commands
    DROP CONSTRAINT IF EXISTS device_runtime_command_type_valid;

ALTER TABLE device_runtime_commands
    ADD CONSTRAINT device_runtime_command_type_valid
        CHECK (
            command_type IN (
                'refresh_configuration',
                'reconnect_network',
                'resync_time',
                'factory_reset',
                'display_message',
                'sync_privacy_consent',
                'firmware_update'
            )
        );

CREATE INDEX IF NOT EXISTS device_runtime_commands_firmware_release_idx
    ON device_runtime_commands ((payload ->> 'release_id'))
    WHERE command_type = 'firmware_update';

-- One active trigger per device/release is enough. A retry or replay should
-- observe the existing durable command instead of queueing another install.
CREATE UNIQUE INDEX IF NOT EXISTS device_runtime_commands_firmware_active_unique
    ON device_runtime_commands (device_id, (payload ->> 'release_id'))
    WHERE command_type = 'firmware_update'
      AND status IN ('pending', 'delivered');
