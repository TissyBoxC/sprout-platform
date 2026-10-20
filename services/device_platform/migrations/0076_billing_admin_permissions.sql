-- The billing console needs its own capability, separate from the existing
-- family and AI-account permissions it must not reuse. Reading orders and
-- reconciliation is an audit activity; approving refunds and editing plans
-- moves money, so the two capabilities stay distinct.
--
-- The catalog check constraint in 0045 is a closed list, so a new identifier
-- is rejected until this constraint is widened. Dropping it first keeps the
-- migration idempotent when the catalog is edited again later.

ALTER TABLE parent_accounts
    DROP CONSTRAINT IF EXISTS parent_accounts_admin_permissions_known;

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
            'audit.read',
            'billing.read',
            'billing.manage'
        ]::TEXT[]
    );
