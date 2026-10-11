-- Wearable location, emergency contacts, home geofence and SOS (P2-09).
--
-- A family owns one location privacy policy, the emergency contacts a child can
-- reach, one bounded home geofence and the SOS history. The platform stores only
-- the bounded metadata it needs: a rounded position, the precision band, the
-- geofence result and the SOS lifecycle. It never stores a raw continuous track,
-- and clearing the policy deletes the retained positions so revoking the
-- location permission stops sampling, upload and display.

CREATE TABLE IF NOT EXISTS wearable_location_policy (
    family_id UUID PRIMARY KEY REFERENCES parent_accounts(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    permission_granted BOOLEAN NOT NULL DEFAULT FALSE,
    precision TEXT NOT NULL DEFAULT 'coarse',
    upload_interval_seconds INT NOT NULL DEFAULT 900,
    retention_days INT NOT NULL DEFAULT 7,
    geofence_radius_meters INT NOT NULL DEFAULT 500,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT wearable_policy_precision_valid
        CHECK (precision IN ('coarse', 'balanced', 'precise')),
    CONSTRAINT wearable_policy_upload_interval_valid
        CHECK (upload_interval_seconds BETWEEN 30 AND 86400),
    CONSTRAINT wearable_policy_retention_valid
        CHECK (retention_days BETWEEN 1 AND 90),
    CONSTRAINT wearable_policy_radius_valid
        CHECK (geofence_radius_meters BETWEEN 50 AND 10000)
);

-- One bounded position per device. A rounded coordinate is stored instead of a
-- track so precision is a data-minimizing choice, not just a display label.

CREATE TABLE IF NOT EXISTS wearable_device_status (
    device_id TEXT PRIMARY KEY REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    location_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    permission_granted BOOLEAN NOT NULL DEFAULT FALSE,
    precision TEXT NOT NULL DEFAULT 'coarse',
    sos_state TEXT NOT NULL DEFAULT 'idle',
    latitude DOUBLE PRECISION,
    longitude DOUBLE PRECISION,
    accuracy_meters DOUBLE PRECISION NOT NULL DEFAULT 0,
    geofence_state TEXT NOT NULL DEFAULT 'unknown',
    last_reported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT wearable_status_precision_valid
        CHECK (precision IN ('coarse', 'balanced', 'precise')),
    CONSTRAINT wearable_status_sos_valid
        CHECK (sos_state IN ('idle', 'armed', 'triggered', 'cancelled')),
    CONSTRAINT wearable_status_geofence_valid
        CHECK (geofence_state IN ('inside', 'outside', 'unknown')),
    CONSTRAINT wearable_status_latitude_valid
        CHECK (latitude IS NULL OR latitude BETWEEN -90 AND 90),
    CONSTRAINT wearable_status_longitude_valid
        CHECK (longitude IS NULL OR longitude BETWEEN -180 AND 180)
);

CREATE INDEX IF NOT EXISTS wearable_device_status_family_idx
    ON wearable_device_status (family_id, device_id);

-- One row per guardian-managed emergency contact, ordered by priority.

CREATE TABLE IF NOT EXISTS wearable_contacts (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    child_id UUID REFERENCES child_profiles(id) ON DELETE SET NULL,
    display_name TEXT NOT NULL,
    phone TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'guardian',
    priority INT NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT wearable_contacts_name_valid
        CHECK (char_length(display_name) BETWEEN 1 AND 40),
    CONSTRAINT wearable_contacts_phone_valid
        CHECK (char_length(phone) BETWEEN 1 AND 32),
    CONSTRAINT wearable_contacts_role_valid
        CHECK (role IN ('guardian', 'family', 'service')),
    CONSTRAINT wearable_contacts_priority_valid
        CHECK (priority >= 0)
);

CREATE INDEX IF NOT EXISTS wearable_contacts_family_idx
    ON wearable_contacts (family_id, priority ASC, created_at DESC);

-- A family address is minimized to one center point plus a radius so the
-- platform never stores a street address.

CREATE TABLE IF NOT EXISTS wearable_geofences (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL UNIQUE REFERENCES parent_accounts(id) ON DELETE CASCADE,
    child_id UUID REFERENCES child_profiles(id) ON DELETE SET NULL,
    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,
    radius_meters INT NOT NULL DEFAULT 500,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT wearable_geofences_latitude_valid
        CHECK (latitude BETWEEN -90 AND 90),
    CONSTRAINT wearable_geofences_longitude_valid
        CHECK (longitude BETWEEN -180 AND 180),
    CONSTRAINT wearable_geofences_radius_valid
        CHECK (radius_meters BETWEEN 50 AND 10000)
);

-- One row per SOS the device raises. A triggered SOS is latched until it is
-- cancelled so a momentary button bounce never clears an emergency.

CREATE TABLE IF NOT EXISTS wearable_sos_events (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    device_id TEXT REFERENCES device_credentials(device_id) ON DELETE SET NULL,
    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,
    state TEXT NOT NULL DEFAULT 'triggered',
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cancelled_at TIMESTAMPTZ,
    CONSTRAINT wearable_sos_state_valid
        CHECK (state IN ('idle', 'armed', 'triggered', 'cancelled')),
    CONSTRAINT wearable_sos_latitude_valid
        CHECK (latitude BETWEEN -90 AND 90),
    CONSTRAINT wearable_sos_longitude_valid
        CHECK (longitude BETWEEN -180 AND 180)
);

CREATE INDEX IF NOT EXISTS wearable_sos_family_idx
    ON wearable_sos_events (family_id, triggered_at DESC);

CREATE INDEX IF NOT EXISTS wearable_sos_active_idx
    ON wearable_sos_events (family_id, device_id)
    WHERE state = 'triggered';
