ALTER TABLE drivers ADD COLUMN owner_id BIGINT REFERENCES users(id) ON DELETE CASCADE;

/* rows from before ownership existed go to the oldest account */
UPDATE drivers SET owner_id = (SELECT id FROM users ORDER BY id LIMIT 1) WHERE owner_id IS NULL;

/* only possible if no accounts exist, and then nobody could own them */
DELETE FROM drivers WHERE owner_id IS NULL;

ALTER TABLE drivers ALTER COLUMN owner_id SET NOT NULL;

/* a phone is unique per owner, so one owner cannot probe another's drivers */
ALTER TABLE drivers DROP CONSTRAINT drivers_phone_key;
ALTER TABLE drivers ADD CONSTRAINT drivers_owner_phone_key UNIQUE (owner_id, phone);

DROP INDEX drivers_status_id_idx;
CREATE INDEX drivers_owner_id_idx ON drivers (owner_id, id);
CREATE INDEX drivers_owner_status_id_idx ON drivers (owner_id, status, id);