package auth

import (
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const Issuer = "trips-api"

var ErrInvalidToken = errors.New("invalid token")

type Service struct {
	secret    []byte
	ttl       time.Duration
	cost      int
	dummyHash []byte
	now       func() time.Time
}

func NewService(secret string, ttl time.Duration, cost int) (*Service, error) {
	if len(secret) < 32 {
		return nil, errors.New("secret must be atleast 32 bytes")
	}
	if ttl <= 0 {
		return nil, errors.New("token ttl must be positive")
	}
	dummy, err := bcrypt.GenerateFromPassword([]byte("not a real password"), cost)
	if err != nil {
		return nil, err
	}
	return &Service{secret: []byte(secret), ttl: ttl, cost: cost, dummyHash: dummy, now: time.Now}, nil
}

func (s *Service) TTL() time.Duration { return s.ttl }

func (s *Service) HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), s.cost)
	return string(h), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// login calls this for unknown emails so response does not reveal which emails have accounts
func (s *Service) BurnTime(pw string) {
	_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(pw))
}

// creates a signed token for a user and returns when it expires
func (s *Service) Issue(userID int64) (string, time.Time, error) {
	now := s.now()
	exp := now.Add(s.ttl)
	claims := jwt.RegisteredClaims{
		Issuer:    Issuer,
		Subject:   strconv.FormatInt(userID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	return raw, exp, err
}

// Parse checks the signature, algorithm, issuer and expiry, and returns the user id.
// Every failure returns the same error, so callers cannot leak the reason.
func (s *Service) Parse(raw string) (int64, error) {
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(raw, claims,
		func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(Issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(s.now),
	)
	if err != nil {
		return 0, ErrInvalidToken
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || id < 1 {
		return 0, ErrInvalidToken
	}
	return id, nil
}
