/* lets trips point at a driver and prove both belong to the same owner */
ALTER TABLE drivers ADD CONSTRAINT drivers_owner_id_id_key UNIQUE (owner_id, id);

CREATE TABLE trips (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    driver_id  BIGINT,
    pickup     TEXT NOT NULL CHECK (length(pickup) BETWEEN 1 AND 200),
    dropoff    TEXT NOT NULL CHECK (length(dropoff) BETWEEN 1 AND 200),
    status     TEXT NOT NULL DEFAULT 'requested'
               CHECK (status IN ('requested', 'accepted', 'in_progress', 'completed', 'cancelled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    /* the database itself refuses a driver that belongs to another owner */
    FOREIGN KEY (owner_id, driver_id) REFERENCES drivers (owner_id, id),

    /* once a trip is accepted, it must have a driver */
    CHECK (status IN ('requested', 'cancelled') OR driver_id IS NOT NULL)
);

CREATE INDEX trips_owner_id_idx ON trips (owner_id, id);
CREATE INDEX trips_owner_status_id_idx ON trips (owner_id, status, id);
CREATE INDEX trips_owner_driver_idx ON trips (owner_id, driver_id);