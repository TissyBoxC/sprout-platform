-- Multi-device, family linkage and content sync (P2-10).
--
-- A family owns device groups, the family linkage that carries messages across
-- its devices, and the content versions that must reach eventual consistency.
-- The platform stores only the metadata it needs: group membership, a message
-- envelope and a content version. Deleting a device from a group only changes
-- that membership and its group assignments; the other devices keep their data,
-- so removing one device never cascades into another device's content.

CREATE TABLE IF NOT EXISTS device_groups (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    route_scope TEXT NOT NULL DEFAULT 'group',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_groups_name_valid
        CHECK (char_length(name) BETWEEN 1 AND 40),
    CONSTRAINT device_groups_scope_valid
        CHECK (route_scope IN ('group', 'family'))
);

CREATE INDEX IF NOT EXISTS device_groups_family_idx
    ON device_groups (family_id, created_at DESC);

-- Membership rows. A device may belong to more than one group in the family.

CREATE TABLE IF NOT EXISTS device_group_members (
    group_id UUID NOT NULL REFERENCES device_groups(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL REFERENCES device_credentials(device_id) ON DELETE CASCADE,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, device_id)
);

CREATE INDEX IF NOT EXISTS device_group_members_family_idx
    ON device_group_members (family_id, device_id);

-- One row per routed message. The body is bounded so a large payload never
-- lands in the routing table.

CREATE TABLE IF NOT EXISTS device_messages (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    source_device TEXT REFERENCES device_credentials(device_id) ON DELETE SET NULL,
    scope TEXT NOT NULL,
    target TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT device_messages_scope_valid
        CHECK (scope IN ('device', 'group', 'family')),
    CONSTRAINT device_messages_body_valid
        CHECK (char_length(body) BETWEEN 1 AND 2048)
);

CREATE INDEX IF NOT EXISTS device_messages_family_idx
    ON device_messages (family_id, created_at DESC);

-- One row per content key, carrying the version that must converge across the
-- family. A same-version tie under a defer strategy increments the conflict
-- count instead of silently applying.

CREATE TABLE IF NOT EXISTS content_sync_states (
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    content_key TEXT NOT NULL,
    version BIGINT NOT NULL DEFAULT 0,
    conflict_count INT NOT NULL DEFAULT 0,
    sync_state TEXT NOT NULL DEFAULT 'pending',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (family_id, content_key),
    CONSTRAINT content_sync_state_valid
        CHECK (sync_state IN ('pending', 'applied', 'conflict', 'failed')),
    CONSTRAINT content_sync_version_valid
        CHECK (version >= 0),
    CONSTRAINT content_sync_conflict_valid
        CHECK (conflict_count >= 0)
);

CREATE INDEX IF NOT EXISTS content_sync_family_idx
    ON content_sync_states (family_id, updated_at DESC);
