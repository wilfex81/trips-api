package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wilfex81/trips-api/internal/store"
)

type tripPageBody struct {
	Data       []store.Trip `json:"data"`
	NextCursor *string      `json:"next_cursor"`
}

func apiDriver(t *testing.T, h http.Handler, phone, status string) store.Driver {
	t.Helper()
	rec := do(h, "POST", "/drivers", fmt.Sprintf(`{"name":"D","phone":%q}`, phone))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	d := decode[store.Driver](t, rec)
	if status != "offline" {
		rec = do(h, "PUT", fmt.Sprintf("/drivers/%d", d.ID),
			fmt.Sprintf(`{"name":"D","phone":%q,"status":%q}`, phone, status))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		d = decode[store.Driver](t, rec)
	}
	return d
}

func apiTrip(t *testing.T, h http.Handler) store.Trip {
	t.Helper()
	rec := do(h, "POST", "/trips", `{"pickup":"Westlands","dropoff":"JKIA"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return decode[store.Trip](t, rec)
}

func tripAction(h http.Handler, id int64, action, body string) *httptest.ResponseRecorder {
	return do(h, "POST", fmt.Sprintf("/trips/%d/%s", id, action), body)
}

func driverStatus(t *testing.T, h http.Handler, id int64) string {
	t.Helper()
	rec := do(h, "GET", fmt.Sprintf("/drivers/%d", id), "")
	require.Equal(t, http.StatusOK, rec.Code)
	return decode[store.Driver](t, rec).Status
}

func getTripPage(t *testing.T, h http.Handler, query string) tripPageBody {
	t.Helper()
	rec := do(h, "GET", "/trips"+query, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return decode[tripPageBody](t, rec)
}

func tripIDs(ts []store.Trip) []int64 {
	out := make([]int64, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}

func TestTripsRequireAuth(t *testing.T) {
	h := newRawRouter()
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/trips", `{"pickup":"A","dropoff":"B"}`},
		{"GET", "/trips", ""},
		{"GET", "/trips/1", ""},
		{"POST", "/trips/1/accept", `{"driver_id":1}`},
		{"POST", "/trips/1/start", ""},
		{"POST", "/trips/1/complete", ""},
		{"POST", "/trips/1/cancel", ""},
	} {
		assert.Equal(t, http.StatusUnauthorized, do(h, tc.method, tc.path, tc.body).Code, "%s %s", tc.method, tc.path)
	}
}

func TestTripLifecycleOverHTTP(t *testing.T) {
	h := newTestRouter()
	d := apiDriver(t, h, "+254700000001", "available")

	rec := do(h, "POST", "/trips", `{"pickup":"Westlands","dropoff":"JKIA"}`)
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "/trips/1", rec.Header().Get("Location"))
	assert.Contains(t, rec.Body.String(), `"driver_id":null`)
	tr := decode[store.Trip](t, rec)
	assert.Equal(t, "requested", tr.Status)

	rec = tripAction(h, tr.ID, "accept", fmt.Sprintf(`{"driver_id":%d}`, d.ID))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	tr = decode[store.Trip](t, rec)
	assert.Equal(t, "accepted", tr.Status)
	require.NotNil(t, tr.DriverID)
	assert.Equal(t, d.ID, *tr.DriverID)
	assert.Equal(t, "on_trip", driverStatus(t, h, d.ID))

	rec = tripAction(h, tr.ID, "start", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "in_progress", decode[store.Trip](t, rec).Status)

	rec = tripAction(h, tr.ID, "complete", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "completed", decode[store.Trip](t, rec).Status)
	assert.Equal(t, "available", driverStatus(t, h, d.ID))

	got := decode[store.Trip](t, do(h, "GET", fmt.Sprintf("/trips/%d", tr.ID), ""))
	assert.Equal(t, "completed", got.Status)
}

func TestInvalidTransitionsOverHTTP(t *testing.T) {
	h := newTestRouter()
	tr := apiTrip(t, h)

	rec := tripAction(h, tr.ID, "start", "")
	assert.Equal(t, http.StatusConflict, rec.Code)
	e := decode[errBody](t, rec)
	assert.Equal(t, "invalid_transition", e.Error.Code)
	assert.Equal(t, "cannot start a trip that is requested", e.Error.Message)

	assert.Equal(t, http.StatusConflict, tripAction(h, tr.ID, "complete", "").Code)

	assert.Equal(t, http.StatusOK, tripAction(h, tr.ID, "cancel", "").Code)
	for _, a := range []string{"start", "complete", "cancel"} {
		assert.Equal(t, http.StatusConflict, tripAction(h, tr.ID, a, "").Code, a)
	}
	assert.Equal(t, http.StatusConflict, tripAction(h, tr.ID, "accept", `{"driver_id":1}`).Code)
}

func TestAcceptErrors(t *testing.T) {
	h := newTestRouter()
	offline := apiDriver(t, h, "+254700000001", "offline")
	free := apiDriver(t, h, "+254700000002", "available")
	tr := apiTrip(t, h)

	// no body at all
	rec := tripAction(h, tr.ID, "accept", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_json", decode[errBody](t, rec).Error.Code)

	for _, body := range []string{`{}`, `{"driver_id":0}`, `{"driver_id":-3}`} {
		rec := tripAction(h, tr.ID, "accept", body)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, body)
		assert.Contains(t, decode[errBody](t, rec).Error.Fields, "driver_id", body)
	}

	rec = tripAction(h, tr.ID, "accept", `{"driver_id":999}`)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "driver not found", decode[errBody](t, rec).Error.Fields["driver_id"])

	rec = tripAction(h, tr.ID, "accept", fmt.Sprintf(`{"driver_id":%d}`, offline.ID))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "driver_unavailable", decode[errBody](t, rec).Error.Code)

	// every failure left the trip alone
	got := decode[store.Trip](t, do(h, "GET", fmt.Sprintf("/trips/%d", tr.ID), ""))
	assert.Equal(t, "requested", got.Status)
	assert.Nil(t, got.DriverID)

	// a driver who is already on a trip is not available for another
	require.Equal(t, http.StatusOK, tripAction(h, tr.ID, "accept", fmt.Sprintf(`{"driver_id":%d}`, free.ID)).Code)
	other := apiTrip(t, h)
	rec = tripAction(h, other.ID, "accept", fmt.Sprintf(`{"driver_id":%d}`, free.ID))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "driver_unavailable", decode[errBody](t, rec).Error.Code)
}

func TestTripOwnershipOverHTTP(t *testing.T) {
	raw := newRawRouter()
	alice, bob := routerFor(raw, 1), routerFor(raw, 2)
	aliceDriver := apiDriver(t, alice, "+254700000001", "available")
	tr := apiTrip(t, alice)

	assert.Equal(t, http.StatusNotFound, do(bob, "GET", fmt.Sprintf("/trips/%d", tr.ID), "").Code)
	for _, a := range []string{"start", "complete", "cancel"} {
		assert.Equal(t, http.StatusNotFound, tripAction(bob, tr.ID, a, "").Code, a)
	}
	assert.Equal(t, http.StatusNotFound,
		tripAction(bob, tr.ID, "accept", fmt.Sprintf(`{"driver_id":%d}`, aliceDriver.ID)).Code)
	assert.JSONEq(t, `{"data":[],"next_cursor":null}`, do(bob, "GET", "/trips", "").Body.String())

	// bob cannot put alice's driver on his own trip
	bobTrip := apiTrip(t, bob)
	rec := tripAction(bob, bobTrip.ID, "accept", fmt.Sprintf(`{"driver_id":%d}`, aliceDriver.ID))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, decode[errBody](t, rec).Error.Fields, "driver_id")

	// alice's trip and driver are untouched
	got := decode[store.Trip](t, do(alice, "GET", fmt.Sprintf("/trips/%d", tr.ID), ""))
	assert.Equal(t, "requested", got.Status)
	assert.Equal(t, "available", driverStatus(t, alice, aliceDriver.ID))
}

func TestTripListFilterAndPaging(t *testing.T) {
	h := newTestRouter()
	for i := 0; i < 3; i++ {
		apiTrip(t, h)
	}
	require.Equal(t, http.StatusOK, tripAction(h, 2, "cancel", "").Code)

	cancelled := getTripPage(t, h, "?status=cancelled")
	assert.Equal(t, []int64{2}, tripIDs(cancelled.Data))
	assert.Nil(t, cancelled.NextCursor)

	p1 := getTripPage(t, h, "?limit=2")
	assert.Equal(t, []int64{1, 2}, tripIDs(p1.Data))
	require.NotNil(t, p1.NextCursor)
	p2 := getTripPage(t, h, "?limit=2&cursor="+*p1.NextCursor)
	assert.Equal(t, []int64{3}, tripIDs(p2.Data))
	assert.Nil(t, p2.NextCursor)

	// each resource accepts only its own statuses
	rec := do(h, "GET", "/trips?status=flying", "")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, decode[errBody](t, rec).Error.Fields["status"], "requested")
	assert.Equal(t, http.StatusUnprocessableEntity, do(h, "GET", "/drivers?status=requested", "").Code)
}

func TestCreateTripValidation(t *testing.T) {
	h := newTestRouter()
	long := strings.Repeat("a", 201)

	tests := []struct {
		name   string
		body   string
		status int
		code   string
		field  string
	}{
		{"missing dropoff", `{"pickup":"A"}`, 422, "validation_failed", "dropoff"},
		{"blank pickup", `{"pickup":"  ","dropoff":"B"}`, 422, "validation_failed", "pickup"},
		{"too long", `{"pickup":"` + long + `","dropoff":"B"}`, 422, "validation_failed", "pickup"},
		{"client cannot set status", `{"pickup":"A","dropoff":"B","status":"completed"}`, 400, "invalid_json", ""},
		{"client cannot set driver", `{"pickup":"A","dropoff":"B","driver_id":5}`, 400, "invalid_json", ""},
		{"not json", `x`, 400, "invalid_json", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(h, "POST", "/trips", tc.body)
			assert.Equal(t, tc.status, rec.Code)
			e := decode[errBody](t, rec)
			assert.Equal(t, tc.code, e.Error.Code)
			if tc.field != "" {
				assert.Contains(t, e.Error.Fields, tc.field)
			}
		})
	}
}

func TestTripRoutesAndIDs(t *testing.T) {
	h := newTestRouter()
	assert.Equal(t, http.StatusBadRequest, do(h, "GET", "/trips/abc", "").Code)
	assert.Equal(t, http.StatusBadRequest, do(h, "POST", "/trips/0/cancel", "").Code)
	assert.Equal(t, http.StatusNotFound, do(h, "POST", "/trips/99/cancel", "").Code)

	// trips change only through their actions
	assert.Equal(t, http.StatusMethodNotAllowed, do(h, "DELETE", "/trips/1", "").Code)
	assert.Equal(t, http.StatusMethodNotAllowed,
		do(h, "PUT", "/trips/1", `{"pickup":"A","dropoff":"B"}`).Code)
}

// a driver that still has trips cannot be deleted: the database says so,
// and the API turns that into a 409
type inUseRepo struct{ *fakeRepo }

func (inUseRepo) Delete(context.Context, int64, int64) error { return store.ErrInUse }

func TestDeleteDriverInUse(t *testing.T) {
	h := routerFor(NewRouter(Deps{DB: fakeDB{}, Drivers: inUseRepo{newFakeRepo()}, Auth: testAuth}), 1)
	rec := do(h, "DELETE", "/drivers/1", "")
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "in_use", decode[errBody](t, rec).Error.Code)
}
