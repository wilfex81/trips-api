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
	"github.com/wilfex81/trips-api/internal/trips"
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
	drivers := newFakeRepo()
	return NewRouter(Deps{
		DB:      fakeDB{},
		Drivers: drivers,
		Trips:   newFakeTrips(drivers),
		Users:   newFakeUsers(),
		Auth:    testAuth,
	})
}

// newTestRouter acts as user 1, which keeps the older tests unchanged.
func newTestRouter() http.Handler { return routerFor(newRawRouter(), 1) }

type fakeTrips struct {
	next    int64
	data    map[int64]store.Trip
	owner   map[int64]int64
	drivers *fakeRepo
}

func newFakeTrips(drivers *fakeRepo) *fakeTrips {
	return &fakeTrips{next: 1, data: map[int64]store.Trip{}, owner: map[int64]int64{}, drivers: drivers}
}

func (f *fakeTrips) mine(ownerID, id int64) (store.Trip, bool) {
	t, ok := f.data[id]
	if !ok || f.owner[id] != ownerID {
		return store.Trip{}, false
	}
	return t, true
}

func (f *fakeTrips) Create(_ context.Context, ownerID int64, pickup, dropoff string) (store.Trip, error) {
	now := time.Now()
	t := store.Trip{ID: f.next, Pickup: pickup, Dropoff: dropoff, Status: "requested", CreatedAt: now, UpdatedAt: now}
	f.data[t.ID] = t
	f.owner[t.ID] = ownerID
	f.next++
	return t, nil
}

func (f *fakeTrips) Get(_ context.Context, ownerID, id int64) (store.Trip, error) {
	t, ok := f.mine(ownerID, id)
	if !ok {
		return store.Trip{}, store.ErrNotFound
	}
	return t, nil
}

func (f *fakeTrips) List(_ context.Context, p store.TripListParams) ([]store.Trip, error) {
	out := make([]store.Trip, 0)
	for id := p.AfterID + 1; id < f.next && len(out) < p.Limit; id++ {
		t, ok := f.mine(p.OwnerID, id)
		if !ok || (p.Status != "" && t.Status != p.Status) {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeTrips) Apply(_ context.Context, ownerID, id int64, a trips.Action, driverID int64) (store.Trip, error) {
	t, ok := f.mine(ownerID, id)
	if !ok {
		return store.Trip{}, store.ErrNotFound
	}
	next, err := trips.Next(trips.Status(t.Status), a)
	if err != nil {
		return store.Trip{}, &store.TransitionError{Action: string(a), From: t.Status}
	}
	if a == trips.Accept {
		d, ok := f.drivers.mine(ownerID, driverID)
		if !ok {
			return store.Trip{}, store.ErrDriverNotFound
		}
		if d.Status != "available" {
			return store.Trip{}, store.ErrDriverUnavailable
		}
		t.DriverID = &driverID
	}
	t.Status = string(next)
	t.UpdatedAt = time.Now()
	f.data[id] = t

	if ds, ok := trips.DriverStatus(a); ok && t.DriverID != nil {
		d := f.drivers.data[*t.DriverID]
		d.Status = ds
		f.drivers.data[*t.DriverID] = d
	}
	return t, nil
}
