package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin/binding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wilfex81/trips-api/internal/store"
)

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

func TestCustomValidatorsRegistered(t *testing.T) {
	registerValidators()

	type probe struct {
		A string `binding:"notblank"`
		B string `binding:"maxbytes=3"`
	}
	assert.NoError(t, binding.Validator.ValidateStruct(&probe{A: "x", B: "abc"}))
	assert.Error(t, binding.Validator.ValidateStruct(&probe{A: "   ", B: "abc"}))
	assert.Error(t, binding.Validator.ValidateStruct(&probe{A: "x", B: "abcd"}))
}

func TestDriversRequireAuth(t *testing.T) {
	h := newRawRouter()
	update := `{"name":"A","phone":"+254700000001","status":"offline"}`

	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/drivers", `{"name":"Ann","phone":"+254700000001"}`},
		{"GET", "/drivers", ""},
		{"GET", "/drivers/1", ""},
		{"PUT", "/drivers/1", update},
		{"DELETE", "/drivers/1", ""},
	} {
		rec := do(h, tc.method, tc.path, tc.body)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", tc.method, tc.path)
	}
	assert.Equal(t, http.StatusOK, do(h, "GET", "/healthz", "").Code) // health checks stay open
}

func TestOwnershipIsolation(t *testing.T) {
	raw := newRawRouter()
	alice := routerFor(raw, 1)
	bob := routerFor(raw, 2)

	rec := do(alice, "POST", "/drivers", `{"name":"Ann","phone":"+254700000001"}`)
	require.Equal(t, http.StatusCreated, rec.Code)

	// bob cannot read, change or delete it, and cannot tell it exists
	assert.Equal(t, http.StatusNotFound, do(bob, "GET", "/drivers/1", "").Code)
	assert.Equal(t, http.StatusNotFound,
		do(bob, "PUT", "/drivers/1", `{"name":"X","phone":"+254700000009","status":"available"}`).Code)
	assert.Equal(t, http.StatusNotFound, do(bob, "DELETE", "/drivers/1", "").Code)
	assert.JSONEq(t, `{"data":[],"next_cursor":null}`, do(bob, "GET", "/drivers", "").Body.String())

	// alice's driver is untouched
	got := decode[store.Driver](t, do(alice, "GET", "/drivers/1", ""))
	assert.Equal(t, "Ann", got.Name)
	assert.Equal(t, "offline", got.Status)

	// the same phone is fine for another owner, but not twice for one owner
	assert.Equal(t, http.StatusCreated, do(bob, "POST", "/drivers", `{"name":"Ann B","phone":"+254700000001"}`).Code)
	assert.Equal(t, http.StatusConflict, do(alice, "POST", "/drivers", `{"name":"Dup","phone":"+254700000001"}`).Code)
}

func TestListShowsOnlyOwnDrivers(t *testing.T) {
	raw := newRawRouter()
	alice := routerFor(raw, 1)
	bob := routerFor(raw, 2)
	seed(alice, 3) // ids 1, 2, 3
	seed(bob, 2)   // ids 4, 5, mixed in with alice's rows

	assert.Equal(t, []int64{4, 5}, ids(getPage(t, bob, "").Data))

	p1 := getPage(t, alice, "?limit=2")
	assert.Equal(t, []int64{1, 2}, ids(p1.Data))
	require.NotNil(t, p1.NextCursor)
	p2 := getPage(t, alice, "?limit=2&cursor="+*p1.NextCursor)
	assert.Equal(t, []int64{3}, ids(p2.Data))
	assert.Nil(t, p2.NextCursor)
}
