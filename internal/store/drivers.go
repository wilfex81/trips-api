package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrInUse    = errors.New("in use")
)

type Driver struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Phone     string    `json:"phone"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type DriverStore struct {
	db *pgxpool.Pool
}

func NewDriverStore(db *pgxpool.Pool) *DriverStore {
	return &DriverStore{db: db}
}

const driverCols = "id, name, phone, status, created_at, updated_at"

func scanDriver(row pgx.Row) (Driver, error) {
	var d Driver
	err := row.Scan(&d.ID, &d.Name, &d.Phone, &d.Status, &d.CreatedAt, &d.UpdatedAt)
	d.CreatedAt, d.UpdatedAt = d.CreatedAt.UTC(), d.UpdatedAt.UTC()
	return d, err
}

// turns database errors into the errors the rest of the app understands
func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique violation
			return ErrConflict
		case "23503": // foreign key violation: something still points at this row
			return ErrInUse
		}
	}
	return err
}

func (s *DriverStore) Create(ctx context.Context, ownerID int64, name, phone string) (Driver, error) {
	d, err := scanDriver(s.db.QueryRow(ctx,
		`INSERT INTO drivers (owner_id, name, phone) VALUES ($1, $2, $3) RETURNING `+driverCols,
		ownerID, name, phone))
	return d, mapErr(err)
}

func (s *DriverStore) Get(ctx context.Context, ownerID, id int64) (Driver, error) {
	d, err := scanDriver(s.db.QueryRow(ctx,
		`SELECT `+driverCols+` FROM drivers WHERE id = $1 AND owner_id = $2`, id, ownerID))
	return d, mapErr(err)
}

type ListParams struct {
	OwnerID int64  // only this owner's drivers
	AfterID int64  // return drivers with an id greater than this
	Status  string // empty means any status
	Limit   int    // maximum rows to return
}

func (s *DriverStore) List(ctx context.Context, p ListParams) ([]Driver, error) {
	rows, err := s.db.Query(ctx,
		`SELECT `+driverCols+` FROM drivers
		  WHERE owner_id = $1 AND id > $2 AND ($3::text = '' OR status = $3)
		  ORDER BY id
		  LIMIT $4`,
		p.OwnerID, p.AfterID, p.Status, p.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Driver, 0)
	for rows.Next() {
		d, err := scanDriver(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *DriverStore) Update(ctx context.Context, ownerID, id int64, name, phone, status string) (Driver, error) {
	d, err := scanDriver(s.db.QueryRow(ctx,
		`UPDATE drivers
		    SET name = $3, phone = $4, status = $5, updated_at = now()
		  WHERE id = $1 AND owner_id = $2
		  RETURNING `+driverCols,
		id, ownerID, name, phone, status))
	return d, mapErr(err)
}

func (s *DriverStore) Delete(ctx context.Context, ownerID, id int64) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM drivers WHERE id = $1 AND owner_id = $2`, id, ownerID)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
