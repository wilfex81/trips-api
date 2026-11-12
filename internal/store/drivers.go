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

func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}

func (s *DriverStore) Create(ctx context.Context, name, phone string) (Driver, error) {
	d, err := scanDriver(s.db.QueryRow(ctx,
		`INSERT INTO drivers (name, phone) VALUES ($1, $2) RETURNING `+driverCols, name,
		phone))
	return d, mapErr(err)
}

func (s *DriverStore) Get(ctx context.Context, id int64) (Driver, error) {
	d, err := scanDriver(s.db.QueryRow(ctx,
		`SELECT `+driverCols+` FROM drivers WHERE id = $1`, id))
	return d, mapErr(err)
}

func (s *DriverStore) List(ctx context.Context) ([]Driver, error) {
	rows, err := s.db.Query(ctx,
		`SELECT `+driverCols+` FROM drivers ORDER BY id LIMIT 100`)
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

func (s *DriverStore) Update(ctx context.Context, id int64, name, phone, status string) (Driver, error) {
	d, err := scanDriver(s.db.QueryRow(ctx,
		`UPDATE drivers
		    SET name = $2, phone = $3, status = $4, updated_at = now()
		  WHERE id = $1
		  RETURNING `+driverCols,
		id, name, phone, status))
	return d, mapErr(err)
}

func (s *DriverStore) Delete(ctx context.Context, id int64) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM drivers WHERE id = $1`, id)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
