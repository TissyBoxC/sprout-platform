-- Durable last-known health for every feature-center capability.
--
-- Health observations are deliberately separate from configuration overlays:
-- resolving a settings read, changing a feature config, or restarting the
-- service must never erase the latest verified result.
CREATE TABLE IF NOT EXISTS platform_feature_health_snapshots (
    feature_id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    checked_at TIMESTAMPTZ NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    trigger TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT platform_feature_health_snapshots_id_valid
        CHECK (feature_id ~ '^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$'),
    CONSTRAINT platform_feature_health_snapshots_status_valid
        CHECK (status IN ('healthy', 'degraded', 'unavailable', 'unknown')),
    CONSTRAINT platform_feature_health_snapshots_trigger_valid
        CHECK (trigger IN ('manual', 'schedule')),
    CONSTRAINT platform_feature_health_snapshots_latency_non_negative
        CHECK (latency_ms >= 0),
    CONSTRAINT platform_feature_health_snapshots_message_length
        CHECK (length(message) <= 240)
);

CREATE INDEX IF NOT EXISTS platform_feature_health_snapshots_checked_idx
    ON platform_feature_health_snapshots(checked_at DESC);
