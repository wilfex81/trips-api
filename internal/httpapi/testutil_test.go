package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/wilfex81/trips-api/internal/auth"
	"github.com/wilfex81/trips-api/internal/store"
)

type pageBody struct {
	Data       []store.Driver `json:"data"`
	NextCursor *string        `json:"next_cursor"`
}

type errBody struct {
	Error struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Fields  map[string]string `json:"fields"`
	} `json:"error"`
}

// fakeRepo is an in-memory DriverRepo that enforces ownership like the SQL does.
type fakeRepo struct {
	next  int64
	data  map[int64]store.Driver
	owner map[int64]int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{next: 1, data: map[int64]store.Driver{}, owner: map[int64]int64{}}
}

// mine returns a driver only if it belongs to ownerID.
func (f *fakeRepo) mine(ownerID, id int64) (store.Driver, bool) {
	d, ok := f.data[id]
	if !ok || f.owner[id] != ownerID {
		return store.Driver{}, false
	}
	return d, true
}

func (f *fakeRepo) phoneTaken(ownerID int64, phone string, except int64) bool {
	for id, d := range f.data {
		if f.owner[id] == ownerID && d.Phone == phone && id != except {
			return true
		}
	}
	return false
}

func (f *fakeRepo) Create(_ context.Context, ownerID int64, name, phone string) (store.Driver, error) {
	if f.phoneTaken(ownerID, phone, 0) {
		return store.Driver{}, store.ErrConflict
	}
	now := time.Now()
	d := store.Driver{ID: f.next, Name: name, Phone: phone, Status: "offline", CreatedAt: now, UpdatedAt: now}
	f.data[d.ID] = d
	f.owner[d.ID] = ownerID
	f.next++
	return d, nil
}

func (f *fakeRepo) Get(_ context.Context, ownerID, id int64) (store.Driver, error) {
	d, ok := f.mine(ownerID, id)
	if !ok {
		return store.Driver{}, store.ErrNotFound
	}
	return d, nil
}

func (f *fakeRepo) List(_ context.Context, p store.ListParams) ([]store.Driver, error) {
	out := make([]store.Driver, 0)
	for id := p.AfterID + 1; id < f.next && len(out) < p.Limit; id++ {
		d, ok := f.mine(p.OwnerID, id)
		if !ok {
			continue
		}
		if p.Status != "" && d.Status != p.Status {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

func (f *fakeRepo) Update(_ context.Context, ownerID, id int64, name, phone, status string) (store.Driver, error) {
	d, ok := f.mine(ownerID, id)
	if !ok {
		return store.Driver{}, store.ErrNotFound
	}
	if f.phoneTaken(ownerID, phone, id) {
		return store.Driver{}, store.ErrConflict
	}
	d.Name, d.Phone, d.Status, d.UpdatedAt = name, phone, status, time.Now()
	f.data[id] = d
	return d, nil
}

func (f *fakeRepo) Delete(_ context.Context, ownerID, id int64) error {
	if _, ok := f.mine(ownerID, id); !ok {
		return store.ErrNotFound
	}
	delete(f.data, id)
	delete(f.owner, id)
	return nil
}

func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v))
	return v
}

var testAuth = mustAuth()

func mustAuth() *auth.Service {
	s, err := auth.NewService(testSecret, time.Hour, bcrypt.MinCost)
	if err != nil {
		panic(err)
	}
	return s
}

// asUser adds a valid token for one user to every request that has no
// Authorization header, so tests can act as that user.
type asUser struct {
	next  http.Handler
	token string
}

func (a asUser) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") == "" {
		r.Header.Set("Authorization", "Bearer "+a.token)
	}
	a.next.ServeHTTP(w, r)
}

func routerFor(next http.Handler, userID int64) http.Handler {
	raw, _, err := testAuth.Issue(userID)
	if err != nil {
		panic(err)
	}
	return asUser{next: next, token: raw}
}

// newRawRouter has no injected login, so requests are anonymous unless they carry a token.
func newRawRouter() http.Handler {
	return NewRouter(Deps{DB: fakeDB{}, Drivers: newFakeRepo(), Users: newFakeUsers(), Auth: testAuth})
}

// newTestRouter acts as user 1, which keeps the older tests unchanged.
func newTestRouter() http.Handler { return routerFor(newRawRouter(), 1) }
