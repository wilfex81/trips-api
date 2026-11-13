package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/wilfex81/trips-api/internal/auth"
	"github.com/wilfex81/trips-api/internal/store"
)

const testSecret = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk" // 32 bytes

// fakeUsers is an in-memory UserRepo.
type fakeUsers struct {
	next int64
	byID map[int64]store.User
}

func newFakeUsers() *fakeUsers {
	return &fakeUsers{next: 1, byID: map[int64]store.User{}}
}

func (f *fakeUsers) Create(_ context.Context, email, hash string) (store.User, error) {
	for _, u := range f.byID {
		if strings.EqualFold(u.Email, email) {
			return store.User{}, store.ErrConflict
		}
	}
	u := store.User{ID: f.next, Email: email, PasswordHash: hash, CreatedAt: time.Now()}
	f.byID[u.ID] = u
	f.next++
	return u, nil
}

func (f *fakeUsers) GetByEmail(_ context.Context, email string) (store.User, error) {
	for _, u := range f.byID {
		if strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	return store.User{}, store.ErrNotFound
}

func (f *fakeUsers) Get(_ context.Context, id int64) (store.User, error) {
	u, ok := f.byID[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return u, nil
}

func newAuthRouter(t *testing.T) (http.Handler, *auth.Service) {
	t.Helper()
	svc, err := auth.NewService(testSecret, time.Hour, bcrypt.MinCost)
	require.NoError(t, err)
	h := NewRouter(Deps{DB: fakeDB{}, Drivers: newFakeRepo(), Users: newFakeUsers(), Auth: svc})
	return h, svc
}

type tokenBody struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

func register(h http.Handler, email, password string) *httptest.ResponseRecorder {
	return do(h, "POST", "/auth/register", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password))
}

func login(h http.Handler, email, password string) *httptest.ResponseRecorder {
	return do(h, "POST", "/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password))
}

func doAuth(h http.Handler, method, path, header string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRegister(t *testing.T) {
	h, _ := newAuthRouter(t)

	rec := register(h, "Ann@Example.COM", "correct horse")
	assert.Equal(t, http.StatusCreated, rec.Code)
	body := decode[map[string]any](t, rec)
	assert.Equal(t, "ann@example.com", body["email"]) // stored lowercase
	assert.NotContains(t, rec.Body.String(), "password")
	assert.NotContains(t, rec.Body.String(), "$2a$") // no bcrypt hash in any response

	rec = register(h, "ANN@example.com", "another password")
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "email_taken", decode[errBody](t, rec).Error.Code)
}

func TestRegisterValidation(t *testing.T) {
	h, _ := newAuthRouter(t)

	tests := []struct {
		name     string
		email    string
		password string
		field    string
	}{
		{"bad email", "not-an-email", "correct horse", "email"},
		{"short password", "a@b.com", "short", "password"},
		{"73 byte password", "a@b.com", strings.Repeat("a", 73), "password"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := register(h, tc.email, tc.password)
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			assert.Contains(t, decode[errBody](t, rec).Error.Fields, tc.field)
		})
	}

	assert.Equal(t, http.StatusCreated, register(h, "ok@b.com", strings.Repeat("a", 72)).Code)
}

func TestLoginAndMe(t *testing.T) {
	h, _ := newAuthRouter(t)
	register(h, "ann@example.com", "correct horse")

	rec := login(h, "ANN@example.com", "correct horse") // email is case insensitive
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	tb := decode[tokenBody](t, rec)
	assert.Equal(t, "Bearer", tb.TokenType)
	assert.Equal(t, 3600, tb.ExpiresIn)
	assert.NotEmpty(t, tb.AccessToken)

	me := doAuth(h, "GET", "/me", "Bearer "+tb.AccessToken)
	assert.Equal(t, http.StatusOK, me.Code)
	assert.Equal(t, "ann@example.com", decode[map[string]any](t, me)["email"])

	// scheme name is case insensitive
	assert.Equal(t, http.StatusOK, doAuth(h, "GET", "/me", "bearer "+tb.AccessToken).Code)
}

func TestLoginFailuresLookIdentical(t *testing.T) {
	h, _ := newAuthRouter(t)
	register(h, "ann@example.com", "correct horse")

	wrong := login(h, "ann@example.com", "wrong password")
	unknown := login(h, "nobody@example.com", "wrong password")

	assert.Equal(t, http.StatusUnauthorized, wrong.Code)
	assert.Equal(t, http.StatusUnauthorized, unknown.Code)
	assert.Equal(t, "invalid_credentials", decode[errBody](t, wrong).Error.Code)
	// an attacker must not be able to tell which emails have accounts
	assert.Equal(t, wrong.Body.String(), unknown.Body.String())
}

func TestProtectedRoute(t *testing.T) {
	h, _ := newAuthRouter(t)

	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    auth.Issuer,
		Subject:   "1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
	}).SignedString([]byte(testSecret))
	require.NoError(t, err)

	headers := map[string]string{
		"no header":    "",
		"basic scheme": "Basic abc",
		"no token":     "Bearer",
		"empty token":  "Bearer ",
		"garbage":      "Bearer garbage",
		"expired":      "Bearer " + expired,
	}
	for name, header := range headers {
		t.Run(name, func(t *testing.T) {
			rec := doAuth(h, "GET", "/me", header)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.NotEmpty(t, rec.Header().Get("WWW-Authenticate"))
			assert.Equal(t, "unauthorized", decode[errBody](t, rec).Error.Code)
		})
	}
}

func TestTokenForDeletedUser(t *testing.T) {
	h, svc := newAuthRouter(t)
	raw, _, err := svc.Issue(999) // a valid token for an account that does not exist
	require.NoError(t, err)

	rec := doAuth(h, "GET", "/me", "Bearer "+raw)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
