-- Notification device-delivery acknowledgement lookup.
--
-- Device acknowledgement arrives by command id. Without this index the
-- notification delivery write scans the delivery table and then joins back to
-- notifications for every device command acknowledgement. Keep it partial to
-- the device channel so inbox and external-channel rows do not enlarge it.

CREATE INDEX IF NOT EXISTS notification_deliveries_command_idx
    ON notification_deliveries(command_id)
    WHERE channel = 'device' AND command_id <> '';
