-- A provisioning token authorises binding an unbound device once. It must
-- never silently transfer an already-bound device to the token holder.
-- Transfers require an explicit owner-authorized unbind and a new token.

CREATE OR REPLACE FUNCTION reject_bound_device_reassignment()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.parent_account_id <> OLD.parent_account_id THEN
        RAISE EXCEPTION 'device already bound to another guardian'
            USING ERRCODE = '23505',
                  CONSTRAINT = 'device_bindings_owner_transfer_forbidden';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS device_bindings_owner_transfer_forbidden
    ON device_bindings;

CREATE TRIGGER device_bindings_owner_transfer_forbidden
BEFORE UPDATE OF parent_account_id ON device_bindings
FOR EACH ROW
EXECUTE FUNCTION reject_bound_device_reassignment();
