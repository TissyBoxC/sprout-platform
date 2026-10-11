-- Platform-wide device offline download policy (P1-06 离线兜底).
--
-- One row is authoritative for the whole fleet: an operator publishes a cache
-- budget and freshness rule, and every bound device reads the same revision.
-- The table is a singleton (singleton IS TRUE) so a concurrent publish can
-- never create a second competing policy. Values are bounded here as well as
-- in the service so a direct write cannot install an unsafe budget.

CREATE TABLE IF NOT EXISTS content_offline_policy (
    singleton BOOLEAN NOT NULL DEFAULT TRUE,
    offline_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    max_cached_packages INTEGER NOT NULL DEFAULT 20,
    max_cache_bytes BIGINT NOT NULL DEFAULT 536870912,
    cache_ttl_hours INTEGER NOT NULL DEFAULT 720,
    download_over_wifi_only BOOLEAN NOT NULL DEFAULT TRUE,
    revision INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT content_offline_policy_singleton PRIMARY KEY (singleton),
    CONSTRAINT content_offline_policy_singleton_true CHECK (singleton),
    CONSTRAINT content_offline_policy_packages_valid
        CHECK (max_cached_packages BETWEEN 1 AND 200),
    CONSTRAINT content_offline_policy_bytes_valid
        CHECK (max_cache_bytes BETWEEN 16777216 AND 8589934592),
    CONSTRAINT content_offline_policy_ttl_valid
        CHECK (cache_ttl_hours BETWEEN 1 AND 8760),
    CONSTRAINT content_offline_policy_revision_non_negative CHECK (revision >= 0)
);
