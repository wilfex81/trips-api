package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wilfex81/rate-limiter/limiter"
)

// scripted answers every check the same way and records the keys it was asked about.
type scripted struct {
	allow bool
	retry time.Duration
	keys  []string
}

func (s *scripted) Allow(key string) (bool, time.Duration) {
	s.keys = append(s.keys, key)
	return s.allow, s.retry
}

func limitedRouter(ip, user Allower) http.Handler {
	drivers := newFakeRepo()
	return NewRouter(Deps{
		DB: fakeDB{}, Drivers: drivers, Trips: newFakeTrips(drivers),
		Users: newFakeUsers(), Auth: testAuth,
		IPLimiter: ip, UserLimiter: user,
	})
}

const loginBody = `{"email":"nobody@example.com","password":"wrong password"}`

func TestAuthRoutesAreLimitedByAddress(t *testing.T) {
	ip := &scripted{allow: false, retry: 90 * time.Second}
	h := limitedRouter(ip, nil)

	for _, path := range []string{"/auth/login", "/auth/register"} {
		rec := do(h, "POST", path, loginBody)
		assert.Equal(t, http.StatusTooManyRequests, rec.Code, path)
		assert.Equal(t, "90", rec.Header().Get("Retry-After"), path)
		assert.Equal(t, "rate_limited", decode[errBody](t, rec).Error.Code, path)
	}
}

func TestLimiterKeyIgnoresForwardedHeader(t *testing.T) {
	ip := &scripted{allow: true}
	h := limitedRouter(ip, nil)

	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "6.6.6.6")
	h.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, []string{"ip:192.0.2.1"}, ip.keys) // the real address, not the faked one
}

func TestTrustedProxyHeaderIsUsed(t *testing.T) {
	ip := &scripted{allow: true}
	h := NewRouter(Deps{
		DB: fakeDB{}, Users: newFakeUsers(), Auth: testAuth,
		IPLimiter: ip, TrustedProxies: []string{"192.0.2.0/24"},
	})

	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "6.6.6.6")
	h.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, []string{"ip:6.6.6.6"}, ip.keys) // this proxy is listed, so its header counts
}

func TestHealthChecksAreNeverLimited(t *testing.T) {
	deny := &scripted{allow: false, retry: time.Minute}
	h := limitedRouter(deny, deny)

	assert.Equal(t, http.StatusOK, do(h, "GET", "/healthz", "").Code)
	assert.Equal(t, http.StatusOK, do(h, "GET", "/readyz", "").Code)
	assert.Empty(t, deny.keys)
}

func TestAPIIsLimitedPerUser(t *testing.T) {
	user := &scripted{allow: true}
	raw := limitedRouter(nil, user)
	alice, bob := routerFor(raw, 1), routerFor(raw, 2)

	do(alice, "GET", "/drivers", "")
	do(bob, "GET", "/trips", "")
	do(alice, "GET", "/me", "")

	assert.Equal(t, []string{"user:1", "user:2", "user:1"}, user.keys)
}

func TestUnauthenticatedRequestsUseNoUserBucket(t *testing.T) {
	user := &scripted{allow: true}
	h := limitedRouter(nil, user)

	assert.Equal(t, http.StatusUnauthorized, do(h, "GET", "/drivers", "").Code)
	assert.Empty(t, user.keys)
}

func TestRetryAfterIsRoundedUpAndNeverZero(t *testing.T) {
	for _, tc := range []struct {
		retry time.Duration
		want  string
	}{
		{0, "1"},
		{200 * time.Millisecond, "1"},
		{1200 * time.Millisecond, "2"},
		{time.Minute, "60"},
	} {
		user := &scripted{allow: false, retry: tc.retry}
		h := routerFor(limitedRouter(nil, user), 1)

		rec := do(h, "GET", "/drivers", "")
		assert.Equal(t, http.StatusTooManyRequests, rec.Code)
		assert.Equal(t, tc.want, rec.Header().Get("Retry-After"), "%v", tc.retry)
	}
}

// the real limiter from the rate-limiter module, not a stand-in
func TestLoginAttemptsRunOut(t *testing.T) {
	store := limiter.NewStore(func() limiter.Limiter {
		l, err := limiter.NewTokenBucket(3, 0.01)
		require.NoError(t, err)
		return l
	}, time.Hour, 0)
	t.Cleanup(store.Close)
	h := limitedRouter(store, nil)

	for i := 1; i <= 3; i++ {
		assert.Equal(t, http.StatusUnauthorized, do(h, "POST", "/auth/login", loginBody).Code, "attempt %d", i)
	}
	rec := do(h, "POST", "/auth/login", loginBody)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
}
