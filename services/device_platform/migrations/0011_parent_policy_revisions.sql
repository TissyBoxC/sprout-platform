CREATE TABLE IF NOT EXISTS parent_policy_revisions (
    family_id UUID PRIMARY KEY REFERENCES parent_accounts(id) ON DELETE CASCADE,
    revision BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT parent_policy_revisions_non_negative CHECK (revision >= 0)
);

INSERT INTO parent_policy_revisions (family_id, revision)
SELECT family_id, COUNT(*)
FROM parent_policies
GROUP BY family_id
ON CONFLICT (family_id) DO NOTHING;

CREATE OR REPLACE FUNCTION bump_parent_policy_revision()
RETURNS TRIGGER AS $$
DECLARE
    target_family_id UUID;
BEGIN
    target_family_id := COALESCE(NEW.family_id, OLD.family_id);
    INSERT INTO parent_policy_revisions (family_id, revision, updated_at)
    VALUES (target_family_id, 1, NOW())
    ON CONFLICT (family_id) DO UPDATE
    SET revision = parent_policy_revisions.revision + 1,
        updated_at = NOW();
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS parent_policies_revision_trigger ON parent_policies;
CREATE TRIGGER parent_policies_revision_trigger
AFTER INSERT OR UPDATE OR DELETE ON parent_policies
FOR EACH ROW EXECUTE FUNCTION bump_parent_policy_revision();
