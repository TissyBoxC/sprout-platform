-- Usage-report retention window.
--
-- The feature center exposes the usage-report data-retention window as an
-- editable capability, and the privacy retention worker prunes both the
-- device-reported and the gateway-recorded usage ledgers to that window. This
-- backfills existing settings documents so a deployment that predates the
-- field still gets a bounded ledger instead of retaining usage days forever.

UPDATE platform_settings
SET value = jsonb_set(
        jsonb_set(
            value,
            '{retention}',
            COALESCE(value -> 'retention', '{}'::JSONB),
            TRUE
        ),
        '{retention,usage_report_days}',
        to_jsonb(365),
        TRUE
    )
WHERE settings_key = 'operations'
  AND NOT (COALESCE(value -> 'retention', '{}'::JSONB) ? 'usage_report_days');
