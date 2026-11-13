package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
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
	return NewRouter(Deps{DB: fakeDB{}, Drivers: newFakeRepo()})
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
	assert.Equal(t, http.StatusUnprocessableEntity, do(h, "POST", "/drivers", `{"name":"Bob"}`).Code)
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
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, http.StatusNotFound,
		do(h, "PUT", "/drivers/99", `{"name":"X","phone":"+254700000009","status":"offline"}`).Code)
}

func (f *fakeRepo) List(_ context.Context, p store.ListParams) ([]store.Driver, error) {
	out := make([]store.Driver, 0)
	for id := p.AfterID + 1; id < f.next && len(out) < p.Limit; id++ {
		d, ok := f.data[id]
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

func TestDeleteDriver(t *testing.T) {
	h := newTestRouter()
	do(h, "POST", "/drivers", `{"name":"Ann","phone":"+254700000001"}`)

	rec := do(h, "DELETE", "/drivers/1", "")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())

	assert.Equal(t, http.StatusNotFound, do(h, "DELETE", "/drivers/1", "").Code)
}

func TestValidationErrors(t *testing.T) {
	h := newTestRouter()
	long := strings.Repeat("a", 101)

	tests := []struct {
		name   string
		body   string
		status int
		code   string
		field  string // a field expected in "fields", empty if none
	}{
		{"missing phone", `{"name":"Ann"}`, 422, "validation_failed", "phone"},
		{"blank name", `{"name":"   ","phone":"+254700000001"}`, 422, "validation_failed", "name"},
		{"bad phone", `{"name":"Ann","phone":"0700000001"}`, 422, "validation_failed", "phone"},
		{"name too long", `{"name":"` + long + `","phone":"+254700000001"}`, 422, "validation_failed", "name"},
		{"unknown field", `{"name":"Ann","phone":"+254700000001","admin":true}`, 400, "invalid_json", ""},
		{"not json", `nope`, 400, "invalid_json", ""},
		{"empty body", ``, 400, "invalid_json", ""},
		{"wrong type", `{"name":5,"phone":"+254700000001"}`, 400, "invalid_json", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(h, "POST", "/drivers", tc.body)
			assert.Equal(t, tc.status, rec.Code)
			e := decode[errBody](t, rec)
			assert.Equal(t, tc.code, e.Error.Code)
			assert.NotEmpty(t, e.Error.Message)
			if tc.field != "" {
				assert.Contains(t, e.Error.Fields, tc.field)
			}
		})
	}
}

func TestRequestTooLarge(t *testing.T) {
	h := newTestRouter()
	big := `{"name":"` + strings.Repeat("a", 2<<20) + `","phone":"+254700000001"}`
	rec := do(h, "POST", "/drivers", big)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Equal(t, "request_too_large", decode[errBody](t, rec).Error.Code)
}

func TestUnknownRouteAndMethod(t *testing.T) {
	h := newTestRouter()

	rec := do(h, "GET", "/nope", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "route_not_found", decode[errBody](t, rec).Error.Code)

	rec = do(h, "PATCH", "/drivers/1", "")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "method_not_allowed", decode[errBody](t, rec).Error.Code)
}

func TestPanicBecomesJSON500(t *testing.T) {
	r := NewRouter(Deps{DB: fakeDB{}, Drivers: newFakeRepo()})
	r.GET("/boom", func(c *gin.Context) { panic("secret internals") })

	rec := do(r, "GET", "/boom", "")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "internal_error", decode[errBody](t, rec).Error.Code)
	assert.NotContains(t, rec.Body.String(), "secret internals") // internals never reach the client
}

func seed(h http.Handler, n int) {
	for i := 1; i <= n; i++ {
		do(h, "POST", "/drivers", fmt.Sprintf(`{"name":"D%d","phone":"+2547000000%02d"}`, i, i))
	}
}

func getPage(t *testing.T, h http.Handler, query string) pageBody {
	t.Helper()
	rec := do(h, "GET", "/drivers"+query, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return decode[pageBody](t, rec)
}

func ids(ds []store.Driver) []int64 {
	out := make([]int64, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.ID)
	}
	return out
}

func TestListEmpty(t *testing.T) {
	rec := do(newTestRouter(), "GET", "/drivers", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"data":[],"next_cursor":null}`, rec.Body.String())
}

func TestListPagination(t *testing.T) {
	h := newTestRouter()
	seed(h, 5)

	p1 := getPage(t, h, "?limit=2")
	assert.Equal(t, []int64{1, 2}, ids(p1.Data))
	require.NotNil(t, p1.NextCursor)

	p2 := getPage(t, h, "?limit=2&cursor="+*p1.NextCursor)
	assert.Equal(t, []int64{3, 4}, ids(p2.Data))
	require.NotNil(t, p2.NextCursor)

	p3 := getPage(t, h, "?limit=2&cursor="+*p2.NextCursor)
	assert.Equal(t, []int64{5}, ids(p3.Data))
	assert.Nil(t, p3.NextCursor)
}

// 4 rows with a page size of 2: the last page is exactly full
func TestListLastPageExactlyFull(t *testing.T) {
	h := newTestRouter()
	seed(h, 4)

	p1 := getPage(t, h, "?limit=2")
	require.NotNil(t, p1.NextCursor)
	p2 := getPage(t, h, "?limit=2&cursor="+*p1.NextCursor)
	assert.Equal(t, []int64{3, 4}, ids(p2.Data))
	assert.Nil(t, p2.NextCursor) // no phantom empty page after this one
}

func TestListFilterByStatusWithPaging(t *testing.T) {
	h := newTestRouter()
	seed(h, 6)
	for _, id := range []int{2, 4, 6} {
		rec := do(h, "PUT", fmt.Sprintf("/drivers/%d", id),
			fmt.Sprintf(`{"name":"D%d","phone":"+2547000000%02d","status":"available"}`, id, id))
		require.Equal(t, http.StatusOK, rec.Code)
	}

	p1 := getPage(t, h, "?status=available&limit=2")
	assert.Equal(t, []int64{2, 4}, ids(p1.Data))
	require.NotNil(t, p1.NextCursor)

	p2 := getPage(t, h, "?status=available&limit=2&cursor="+*p1.NextCursor)
	assert.Equal(t, []int64{6}, ids(p2.Data))
	assert.Nil(t, p2.NextCursor)
}

func TestListQueryErrors(t *testing.T) {
	h := newTestRouter()
	tests := []struct {
		query  string
		status int
		code   string
		field  string
	}{
		{"?limit=0", 422, "validation_failed", "limit"},
		{"?limit=101", 422, "validation_failed", "limit"},
		{"?limit=abc", 422, "validation_failed", "limit"},
		{"?status=flying", 422, "validation_failed", "status"},
		{"?cursor=!!!", 400, "invalid_cursor", ""},
		{"?cursor=YWJj", 400, "invalid_cursor", ""}, // valid base64, but not a number
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			rec := do(h, "GET", "/drivers"+tc.query, "")
			assert.Equal(t, tc.status, rec.Code)
			e := decode[errBody](t, rec)
			assert.Equal(t, tc.code, e.Error.Code)
			if tc.field != "" {
				assert.Contains(t, e.Error.Fields, tc.field)
			}
		})
	}
}
