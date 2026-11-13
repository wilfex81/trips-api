package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type UserStore struct {
	db *pgxpool.Pool
}

func NewUserStore(db *pgxpool.Pool) *UserStore {
	return &UserStore{db: db}
}

const userCols = "id, email, password_hash, created_at"

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	u.CreatedAt = u.CreatedAt.UTC()
	return u, err
}

func (s *UserStore) Create(ctx context.Context, email, passwordHash string) (User, error) {
	u, err := scanUser(s.db.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING `+userCols,
		email, passwordHash))
	return u, mapErr(err)
}

func (s *UserStore) GetByEmail(ctx context.Context, email string) (User, error) {
	u, err := scanUser(s.db.QueryRow(ctx,
		`SELECT `+userCols+` FROM users WHERE lower(email) = lower($1)`, email))
	return u, mapErr(err)
}

func (s *UserStore) Get(ctx context.Context, id int64) (User, error) {
	u, err := scanUser(s.db.QueryRow(ctx,
		`SELECT `+userCols+` FROM users WHERE id = $1`, id))
	return u, mapErr(err)
}
