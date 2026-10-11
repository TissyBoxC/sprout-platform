-- Administrator operation-level permissions (P0-10 least privilege).
--
-- Until now every administrator shared one coarse role, so an operator who
-- only reviews content could also manage devices, storage and service
-- upgrades. The platform now stores an explicit permission set per
-- administrator account. An empty set means "authenticated but no management
-- capability"; the wildcard '*' keeps existing administrators working.
--
-- The column defaults to an empty set so a freshly inserted row never gains
-- authority implicitly. Existing administrators are backfilled with the
-- wildcard for backward compatibility, and the bootstrap path grants the
-- wildcard explicitly to the first administrator.

ALTER TABLE parent_accounts
    ADD COLUMN IF NOT EXISTS admin_permissions TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];

-- Backfill only historical administrators; guardians keep an empty set.
UPDATE parent_accounts
SET admin_permissions = ARRAY['*']::TEXT[]
WHERE role = 'admin'
  AND admin_permissions = ARRAY[]::TEXT[];

-- The catalog mirrors domain.AdminPermissionCatalog. The constraint blocks a
-- typo or a stale client from persisting an unknown or retired permission, so
-- an authorization check can never silently pass on an unrecognized value.
ALTER TABLE parent_accounts
    ADD CONSTRAINT parent_accounts_admin_permissions_known CHECK (
        admin_permissions <@ ARRAY[
            '*',
            'admins.read',
            'admins.manage',
            'devices.read',
            'devices.manage',
            'family.read',
            'family.manage',
            'ai_accounts.read',
            'ai_accounts.manage',
            'ota.read',
            'ota.manage',
            'storage.read',
            'storage.manage',
            'content.read',
            'content.review',
            'service.read',
            'service.upgrade',
            'feature_center.read',
            'feature_center.manage',
            'settings.read',
            'settings.manage',
            'notifications.read',
            'notifications.manage',
            'stats.read',
            'audit.read'
        ]::TEXT[]
    );
