package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

const testSecret = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk" // 32 bytes

func newTestService(t *testing.T) *Service {
	t.Helper()
	s, err := NewService(testSecret, time.Hour, bcrypt.MinCost)
	require.NoError(t, err)
	return s
}

func TestNewServiceValidation(t *testing.T) {
	_, err := NewService("short", time.Hour, bcrypt.MinCost)
	assert.Error(t, err)
	_, err = NewService(testSecret, 0, bcrypt.MinCost)
	assert.Error(t, err)
}

func TestPasswordHashing(t *testing.T) {
	s := newTestService(t)
	h1, err := s.HashPassword("correct horse")
	require.NoError(t, err)
	h2, err := s.HashPassword("correct horse")
	require.NoError(t, err)

	assert.NotEqual(t, "correct horse", h1)
	assert.NotEqual(t, h1, h2) // each hash gets its own random salt
	assert.True(t, s.CheckPassword(h1, "correct horse"))
	assert.False(t, s.CheckPassword(h1, "wrong"))
	assert.False(t, s.CheckPassword("not a hash", "correct horse"))
}

func TestTokenRoundTrip(t *testing.T) {
	s := newTestService(t)
	raw, exp, err := s.Issue(42)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(time.Hour), exp, 5*time.Second)

	id, err := s.Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, int64(42), id)
}

func TestTokenExpires(t *testing.T) {
	s := newTestService(t)
	raw, _, err := s.Issue(1)
	require.NoError(t, err)

	s.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	_, err = s.Parse(raw)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestRejectedTokens(t *testing.T) {
	s := newTestService(t)
	good, _, err := s.Issue(1)
	require.NoError(t, err)

	valid := jwt.RegisteredClaims{
		Issuer:    Issuer,
		Subject:   "1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	sign := func(claims jwt.RegisteredClaims, secret string) string {
		raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
		require.NoError(t, err)
		return raw
	}

	noExp := valid
	noExp.ExpiresAt = nil
	wrongIss := valid
	wrongIss.Issuer = "someone-else"
	badSub := valid
	badSub.Subject = "abc"
	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, valid).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	tests := map[string]string{
		"empty":               "",
		"garbage":             "not.a.token",
		"tampered":            good[:len(good)-2] + "xx",
		"wrong secret":        sign(valid, strings.Repeat("z", 32)),
		"no expiry":           sign(noExp, testSecret),
		"wrong issuer":        sign(wrongIss, testSecret),
		"non numeric subject": sign(badSub, testSecret),
		"alg none":            none,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := s.Parse(raw)
			assert.ErrorIs(t, err, ErrInvalidToken)
		})
	}
}
