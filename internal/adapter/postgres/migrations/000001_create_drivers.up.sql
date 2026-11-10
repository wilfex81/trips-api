CREATE TABLE drivers (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT NOT NULL CHECK (length(name) > 0),
    phone      TEXT NOT NULL UNIQUE,
    status     TEXT NOT NULL DEFAULT 'offline'
               CHECK (status IN ('offline', 'available', 'on_trip')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);