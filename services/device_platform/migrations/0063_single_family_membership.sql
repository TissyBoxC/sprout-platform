-- Product requirement: one guardian account belongs to exactly one family.
-- Family APIs resolve the actual family through family_members and never
-- assume the account ID equals the family ID. Keep that ownership unambiguous
-- in the database so invitation acceptance and administrative flows cannot
-- create a second membership for the same guardian.

DO $$
DECLARE
    conflicting_users TEXT;
BEGIN
    SELECT string_agg(format('%s (%s memberships)', user_id, membership_count), ', ')
      INTO conflicting_users
      FROM (
          SELECT user_id, count(*) AS membership_count
            FROM family_members
           GROUP BY user_id
          HAVING count(*) > 1
      ) duplicates;

    IF conflicting_users IS NOT NULL THEN
        RAISE EXCEPTION
            'cannot enforce one family per guardian; resolve duplicate memberships first: %',
            conflicting_users
            USING ERRCODE = '23505';
    END IF;
END
$$;

CREATE UNIQUE INDEX IF NOT EXISTS family_members_single_family_per_user_idx
    ON family_members (user_id);
