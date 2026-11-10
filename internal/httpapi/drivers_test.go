package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wilfex81/trips-api/internal/store"
)

// fakeRepo is an in-memory DriverRepo, so handler tests need no database.
type fakeRepo struct {
	next int64
	data map[int64]store.Driver
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{next: 1, data: map[int64]store.Driver{}}
}

func (f *fakeRepo) phoneTaken(phone string, except int64) bool {
	for id, d := range f.data {
		if d.Phone == phone && id != except {
			return true
		}
	}
	return false
}

func (f *fakeRepo) Create(_ context.Context, name, phone string) (store.Driver, error) {
	if f.phoneTaken(phone, 0) {
		return store.Driver{}, store.ErrConflict
	}
	now := time.Now()
	d := store.Driver{ID: f.next, Name: name, Phone: phone, Status: "offline", CreatedAt: now, UpdatedAt: now}
	f.data[d.ID] = d
	f.next++
	return d, nil
}

func (f *fakeRepo) Get(_ context.Context, id int64) (store.Driver, error) {
	d, ok := f.data[id]
	if !ok {
		return store.Driver{}, store.ErrNotFound
	}
	return d, nil
}

func (f *fakeRepo) List(context.Context) ([]store.Driver, error) {
	out := make([]store.Driver, 0)
	for id := int64(1); id < f.next; id++ {
		if d, ok := f.data[id]; ok {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakeRepo) Update(_ context.Context, id int64, name, phone, status string) (store.Driver, error) {
	d, ok := f.data[id]
	if !ok {
		return store.Driver{}, store.ErrNotFound
	}
	if f.phoneTaken(phone, id) {
		return store.Driver{}, store.ErrConflict
	}
	d.Name, d.Phone, d.Status, d.UpdatedAt = name, phone, status, time.Now()
	f.data[id] = d
	return d, nil
}

func (f *fakeRepo) Delete(_ context.Context, id int64) error {
	if _, ok := f.data[id]; !ok {
		return store.ErrNotFound
	}
	delete(f.data, id)
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

func newTestRouter() http.Handler {
	return NewRouter(fakeDB{}, newFakeRepo())
}

func TestCreateDriver(t *testing.T) {
	h := newTestRouter()

	rec := do(h, "POST", "/drivers", `{"name":"Ann","phone":"+254700000001"}`)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "/drivers/1", rec.Header().Get("Location"))
	d := decode[store.Driver](t, rec)
	assert.Equal(t, "Ann", d.Name)
	assert.Equal(t, "offline", d.Status)

	// same phone again
	rec = do(h, "POST", "/drivers", `{"name":"Bob","phone":"+254700000001"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)

	// missing phone, and not JSON at all
	assert.Equal(t, http.StatusBadRequest, do(h, "POST", "/drivers", `{"name":"Bob"}`).Code)
	assert.Equal(t, http.StatusBadRequest, do(h, "POST", "/drivers", `not json`).Code)
}

func TestGetDriver(t *testing.T) {
	h := newTestRouter()
	do(h, "POST", "/drivers", `{"name":"Ann","phone":"+254700000001"}`)

	assert.Equal(t, http.StatusOK, do(h, "GET", "/drivers/1", "").Code)
	assert.Equal(t, http.StatusNotFound, do(h, "GET", "/drivers/99", "").Code)
	assert.Equal(t, http.StatusBadRequest, do(h, "GET", "/drivers/abc", "").Code)
	assert.Equal(t, http.StatusBadRequest, do(h, "GET", "/drivers/0", "").Code)
}

func TestUpdateDriver(t *testing.T) {
	h := newTestRouter()
	do(h, "POST", "/drivers", `{"name":"Ann","phone":"+254700000001"}`)

	rec := do(h, "PUT", "/drivers/1", `{"name":"Ann K","phone":"+254700000001","status":"available"}`)
	assert.Equal(t, http.StatusOK, rec.Code)
	d := decode[store.Driver](t, rec)
	assert.Equal(t, "Ann K", d.Name)
	assert.Equal(t, "available", d.Status)

	// status outside the allowed list
	rec = do(h, "PUT", "/drivers/1", `{"name":"Ann","phone":"+254700000001","status":"flying"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	assert.Equal(t, http.StatusNotFound,
		do(h, "PUT", "/drivers/99", `{"name":"X","phone":"1","status":"offline"}`).Code)
}

func TestListDrivers(t *testing.T) {
	h := newTestRouter()

	// an empty list must be [] and never null
	rec := do(h, "GET", "/drivers", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `[]`, rec.Body.String())

	do(h, "POST", "/drivers", `{"name":"Ann","phone":"+254700000001"}`)
	do(h, "POST", "/drivers", `{"name":"Bob","phone":"+254700000002"}`)
	ds := decode[[]store.Driver](t, do(h, "GET", "/drivers", ""))
	assert.Len(t, ds, 2)
}

func TestDeleteDriver(t *testing.T) {
	h := newTestRouter()
	do(h, "POST", "/drivers", `{"name":"Ann","phone":"+254700000001"}`)

	rec := do(h, "DELETE", "/drivers/1", "")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())

	assert.Equal(t, http.StatusNotFound, do(h, "DELETE", "/drivers/1", "").Code)
}
