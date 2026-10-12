-- Platform-level voice conversation defaults.
--
-- The feature center exposes the realtime voice profile as an editable
-- capability. These values are the defaults applied when a new child policy
-- is provisioned; each child policy remains independently editable and is
-- still aggregated with the most restrictive rule at runtime.

UPDATE platform_settings
SET value = value || jsonb_build_object(
    'voice',
    jsonb_build_object(
        'continuous_conversation_enabled', TRUE,
        'idle_window_seconds', 8,
        'barge_in_enabled', TRUE,
        'far_field_enabled', TRUE
    )
)
WHERE settings_key = 'operations'
  AND NOT (value ? 'voice');

