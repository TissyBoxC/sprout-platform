-- Family membership and invitations (roadmap 2.2 identity and permissions).
--
-- A family is the guardian account that owns children, devices and policies,
-- so family_id keeps referencing parent_accounts(id). Membership links those
-- accounts to a family with an explicit role, and at most one member may hold
-- the owner role: the partial unique index enforces it in the database instead
-- of trusting every write path.

CREATE TABLE IF NOT EXISTS family_members (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'guardian',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT family_members_role_valid
        CHECK (role IN ('owner', 'guardian', 'viewer')),
    CONSTRAINT family_members_unique_user
        UNIQUE (family_id, user_id)
);

CREATE INDEX IF NOT EXISTS family_members_family_idx
    ON family_members (family_id, created_at ASC);

-- Exactly one owner per family. A partial index keeps the uniqueness scoped to
-- the owner role so guardians and viewers stay unconstrained.
CREATE UNIQUE INDEX IF NOT EXISTS family_members_single_owner_idx
    ON family_members (family_id)
    WHERE role = 'owner';

-- Pending, accepted and revoked invitations to join a family. An invitation is
-- email addressed and role bounded; the owner role is never issued here, which
-- the constraint mirrors from the domain layer.
CREATE TABLE IF NOT EXISTS family_invitations (
    id UUID PRIMARY KEY,
    family_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'guardian',
    status TEXT NOT NULL DEFAULT 'pending',
    invited_by_user_id UUID NOT NULL REFERENCES parent_accounts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    CONSTRAINT family_invitations_role_valid
        CHECK (role IN ('guardian', 'viewer')),
    CONSTRAINT family_invitations_status_valid
        CHECK (status IN ('pending', 'accepted', 'revoked')),
    CONSTRAINT family_invitations_email_valid
        CHECK (position('@' in email) > 1)
);

CREATE INDEX IF NOT EXISTS family_invitations_family_idx
    ON family_invitations (family_id, created_at DESC);

-- At most one pending invitation per email per family so a repeated invite
-- cannot fan out into several joinable rows.
CREATE UNIQUE INDEX IF NOT EXISTS family_invitations_pending_unique_idx
    ON family_invitations (family_id, lower(email))
    WHERE status = 'pending';
