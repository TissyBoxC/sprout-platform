-- Raw audio is not persisted by the platform. Keep the public retention
-- disclosure aligned with that invariant, including settings documents written
-- by older console versions that exposed an editable audio window.

UPDATE platform_settings
SET value = jsonb_set(
        value,
        '{retention,audio_days}',
        '0'::jsonb,
        true
    ),
    updated_at = NOW()
WHERE settings_key = 'operations'
  AND value ? 'retention'
  AND value->'retention' ? 'audio_days'
  AND value->'retention'->'audio_days' <> '0'::jsonb;
