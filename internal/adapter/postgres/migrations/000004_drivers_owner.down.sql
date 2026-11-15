DROP INDEX drivers_owner_status_id_idx;
DROP INDEX drivers_owner_id_idx;
ALTER TABLE drivers DROP CONSTRAINT drivers_owner_phone_key;
ALTER TABLE drivers DROP COLUMN owner_id;
ALTER TABLE drivers ADD CONSTRAINT drivers_phone_key UNIQUE (phone);
CREATE INDEX drivers_status_id_idx ON drivers (status, id);