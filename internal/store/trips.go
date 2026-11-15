package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wilfex81/trips-api/internal/trips"
)

var (
	ErrDriverNotFound    = errors.New("driver not found")
	ErrDriverUnavailable = errors.New("driver unavailable")
)

// Shows which action was refused and the status that blocked it
type TransitionError struct {
	Action string
	From   string
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("cannot %s a trip that is %s", e.Action, e.From)
}

type Trip struct {
	ID        int64     `json:"id"`
	DriverID  *int64    `json:"driver_id"`
	Pickup    string    `json:"pickup"`
	Dropoff   string    `json:"dropoff"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type TripStore struct {
	db *pgxpool.Pool
}

func NewTripStore(db *pgxpool.Pool) *TripStore {
	return &TripStore{db: db}
}

const tripCols = "id, driver_id, pickup, dropoff, status, created_at, updated_at"

func scanTrip(row pgx.Row) (Trip, error) {
	var t Trip
	err := row.Scan(&t.ID, &t.DriverID, &t.Pickup, &t.Dropoff, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	t.CreatedAt, t.UpdatedAt = t.CreatedAt.UTC(), t.UpdatedAt.UTC()
	return t, err
}

func (s *TripStore) Create(ctx context.Context, ownerID int64, pickup, dropoff string) (Trip, error) {
	t, err := scanTrip(s.db.QueryRow(ctx,
		`INSERT INTO trips (owner_id, pickup, dropoff) VALUES ($1, $2, $3) RETURNING `+tripCols,
		ownerID, pickup, dropoff))
	return t, mapErr(err)
}

func (s *TripStore) Get(ctx context.Context, ownerID, id int64) (Trip, error) {
	t, err := scanTrip(s.db.QueryRow(ctx,
		`SELECT `+tripCols+` FROM trips WHERE id = $1 AND owner_id = $2`, id, ownerID))
	return t, mapErr(err)
}

type TripListParams struct {
	OwnerID int64
	AfterID int64
	Status  string // empty means any status
	Limit   int
}

func (s *TripStore) List(ctx context.Context, p TripListParams) ([]Trip, error) {
	rows, err := s.db.Query(ctx,
		`SELECT `+tripCols+` FROM trips
		  WHERE owner_id = $1 AND id > $2 AND ($3::text = '' OR status = $3)
		  ORDER BY id
		  LIMIT $4`,
		p.OwnerID, p.AfterID, p.Status, p.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Trip, 0)
	for rows.Next() {
		t, err := scanTrip(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Apply performs one action on a trip, and on its driver, as a single
// all-or-nothing step. driverID is only used by Accept.
func (s *TripStore) Apply(ctx context.Context, ownerID, id int64, action trips.Action, driverID int64) (Trip, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Trip{}, err
	}
	defer tx.Rollback(ctx) // does nothing once Commit has succeeded

	// 1. Lock the trip row. A second request for the same trip waits here
	//    until we commit, then sees the new status.
	t, err := scanTrip(tx.QueryRow(ctx,
		`SELECT `+tripCols+` FROM trips WHERE id = $1 AND owner_id = $2 FOR UPDATE`,
		id, ownerID))
	if err != nil {
		return Trip{}, mapErr(err)
	}

	// 2. Ask the rules, with the status we just locked.
	next, err := trips.Next(trips.Status(t.Status), action)
	if err != nil {
		return Trip{}, &TransitionError{Action: string(action), From: t.Status}
	}

	// 3. Accepting needs a driver of this owner who is free. Lock that row
	//    too, always after the trip, so two requests can never deadlock.
	if action == trips.Accept {
		var status string
		err := tx.QueryRow(ctx,
			`SELECT status FROM drivers WHERE id = $1 AND owner_id = $2 FOR UPDATE`,
			driverID, ownerID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return Trip{}, ErrDriverNotFound
		}
		if err != nil {
			return Trip{}, err
		}
		if status != "available" {
			return Trip{}, ErrDriverUnavailable
		}
		t.DriverID = &driverID
	}

	// 4. Save the trip.
	t, err = scanTrip(tx.QueryRow(ctx,
		`UPDATE trips SET status = $3, driver_id = $4, updated_at = now()
		  WHERE id = $1 AND owner_id = $2
		  RETURNING `+tripCols,
		id, ownerID, string(next), t.DriverID))
	if err != nil {
		return Trip{}, mapErr(err)
	}

	// 5. Move the driver along with it.
	if ds, ok := trips.DriverStatus(action); ok && t.DriverID != nil {
		if _, err := tx.Exec(ctx,
			`UPDATE drivers SET status = $3, updated_at = now() WHERE id = $1 AND owner_id = $2`,
			*t.DriverID, ownerID, ds); err != nil {
			return Trip{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Trip{}, err
	}
	return t, nil
}
