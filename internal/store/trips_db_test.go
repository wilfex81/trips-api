package store

import (
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wilfex81/trips-api/internal/trips"
)

type fixture struct {
	ctx       context.Context
	db        *pgxpool.Pool
	drivers   *DriverStore
	tripStore *TripStore
}

// newFixture connects to TEST_DATABASE_URL and applies the migrations.
// The test is skipped when the variable is not set.
func newFixture(t *testing.T) fixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run database tests")
	}
	require.NoError(t, Migrate(dsn))
	db, err := Connect(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	return fixture{ctx: context.Background(), db: db, drivers: NewDriverStore(db), tripStore: NewTripStore(db)}
}

// newOwner creates a user for one test. Deleting it afterwards also removes
// its drivers and trips, so tests leave nothing behind.
func newOwner(t *testing.T, db *pgxpool.Pool) int64 {
	t.Helper()
	email := "test-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "@example.com"
	u, err := NewUserStore(db).Create(context.Background(), email, "not-a-real-hash")
	require.NoError(t, err)
	t.Cleanup(func() { db.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, u.ID) })
	return u.ID
}

func (f fixture) availableDriver(t *testing.T, owner int64, phone string) Driver {
	t.Helper()
	d, err := f.drivers.Create(f.ctx, owner, "Driver "+phone, phone)
	require.NoError(t, err)
	d, err = f.drivers.Update(f.ctx, owner, d.ID, d.Name, d.Phone, "available")
	require.NoError(t, err)
	return d
}

func (f fixture) newTrip(t *testing.T, owner int64) Trip {
	t.Helper()
	tr, err := f.tripStore.Create(f.ctx, owner, "Westlands", "JKIA")
	require.NoError(t, err)
	return tr
}

func (f fixture) driverStatus(t *testing.T, owner, id int64) string {
	t.Helper()
	d, err := f.drivers.Get(f.ctx, owner, id)
	require.NoError(t, err)
	return d.Status
}

func (f fixture) apply(t *testing.T, owner, id int64, a trips.Action, driverID int64) Trip {
	t.Helper()
	tr, err := f.tripStore.Apply(f.ctx, owner, id, a, driverID)
	require.NoError(t, err)
	return tr
}

func TestTripLifecycle(t *testing.T) {
	f := newFixture(t)
	owner := newOwner(t, f.db)
	d := f.availableDriver(t, owner, "+254700000001")

	tr := f.newTrip(t, owner)
	assert.Equal(t, "requested", tr.Status)
	assert.Nil(t, tr.DriverID)

	tr = f.apply(t, owner, tr.ID, trips.Accept, d.ID)
	assert.Equal(t, "accepted", tr.Status)
	require.NotNil(t, tr.DriverID)
	assert.Equal(t, d.ID, *tr.DriverID)
	assert.Equal(t, "on_trip", f.driverStatus(t, owner, d.ID))

	tr = f.apply(t, owner, tr.ID, trips.Start, 0)
	assert.Equal(t, "in_progress", tr.Status)
	assert.Equal(t, "on_trip", f.driverStatus(t, owner, d.ID))

	tr = f.apply(t, owner, tr.ID, trips.Complete, 0)
	assert.Equal(t, "completed", tr.Status)
	assert.Equal(t, "available", f.driverStatus(t, owner, d.ID))
}

func TestCancelFreesDriver(t *testing.T) {
	f := newFixture(t)
	owner := newOwner(t, f.db)
	d := f.availableDriver(t, owner, "+254700000001")

	// cancelling before anyone accepts has no driver to free
	tr := f.newTrip(t, owner)
	tr = f.apply(t, owner, tr.ID, trips.Cancel, 0)
	assert.Equal(t, "cancelled", tr.Status)
	assert.Nil(t, tr.DriverID)

	// cancelling after accepting releases the driver
	tr = f.newTrip(t, owner)
	f.apply(t, owner, tr.ID, trips.Accept, d.ID)
	require.Equal(t, "on_trip", f.driverStatus(t, owner, d.ID))
	tr = f.apply(t, owner, tr.ID, trips.Cancel, 0)
	assert.Equal(t, "cancelled", tr.Status)
	assert.Equal(t, "available", f.driverStatus(t, owner, d.ID))
}

func TestRejectedTransitions(t *testing.T) {
	f := newFixture(t)
	owner := newOwner(t, f.db)
	tr := f.newTrip(t, owner)

	refused := func(a trips.Action, from string) {
		t.Helper()
		_, err := f.tripStore.Apply(f.ctx, owner, tr.ID, a, 0)
		var te *TransitionError
		require.ErrorAs(t, err, &te, "%s from %s", a, from)
		assert.Equal(t, string(a), te.Action)
		assert.Equal(t, from, te.From)
	}

	// nobody accepted it yet
	refused(trips.Start, "requested")
	refused(trips.Complete, "requested")

	// once cancelled, nothing works any more
	f.apply(t, owner, tr.ID, trips.Cancel, 0)
	for _, a := range []trips.Action{trips.Accept, trips.Start, trips.Complete, trips.Cancel} {
		refused(a, "cancelled")
	}

	// a refused action changed nothing
	got, err := f.tripStore.Get(f.ctx, owner, tr.ID)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", got.Status)
}

func TestDriverMustExistAndBeAvailable(t *testing.T) {
	f := newFixture(t)
	owner := newOwner(t, f.db)
	tr := f.newTrip(t, owner)

	_, err := f.tripStore.Apply(f.ctx, owner, tr.ID, trips.Accept, 999999999)
	assert.ErrorIs(t, err, ErrDriverNotFound)

	// new drivers start offline
	offline, err := f.drivers.Create(f.ctx, owner, "Offline", "+254700000002")
	require.NoError(t, err)
	_, err = f.tripStore.Apply(f.ctx, owner, tr.ID, trips.Accept, offline.ID)
	assert.ErrorIs(t, err, ErrDriverUnavailable)

	// the failed attempts left the trip alone
	got, err := f.tripStore.Get(f.ctx, owner, tr.ID)
	require.NoError(t, err)
	assert.Equal(t, "requested", got.Status)
	assert.Nil(t, got.DriverID)
}

func TestTripsAreOwnerScoped(t *testing.T) {
	f := newFixture(t)
	alice := newOwner(t, f.db)
	bob := newOwner(t, f.db)
	bobDriver := f.availableDriver(t, bob, "+254700000001")
	aliceTrip := f.newTrip(t, alice)

	// alice cannot use bob's driver, and cannot tell it exists
	_, err := f.tripStore.Apply(f.ctx, alice, aliceTrip.ID, trips.Accept, bobDriver.ID)
	assert.ErrorIs(t, err, ErrDriverNotFound)

	// bob cannot see or change alice's trip
	_, err = f.tripStore.Get(f.ctx, bob, aliceTrip.ID)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = f.tripStore.Apply(f.ctx, bob, aliceTrip.ID, trips.Cancel, 0)
	assert.ErrorIs(t, err, ErrNotFound)

	// even raw SQL cannot cross owners: the composite foreign key refuses it
	_, err = f.db.Exec(f.ctx,
		`UPDATE trips SET status = 'accepted', driver_id = $1 WHERE id = $2`,
		bobDriver.ID, aliceTrip.ID)
	assert.Error(t, err)

	// a driver with trips cannot be deleted
	d := f.availableDriver(t, alice, "+254700000003")
	tr := f.newTrip(t, alice)
	f.apply(t, alice, tr.ID, trips.Accept, d.ID)
	assert.ErrorIs(t, f.drivers.Delete(f.ctx, alice, d.ID), ErrInUse)
}

// ten trips want the same driver at the same moment: exactly one wins
func TestOneDriverCannotTakeTwoTripsAtOnce(t *testing.T) {
	f := newFixture(t)
	owner := newOwner(t, f.db)
	d := f.availableDriver(t, owner, "+254700000001")

	const n = 10
	tripIDs := make([]int64, n)
	for i := range tripIDs {
		tripIDs[i] = f.newTrip(t, owner).ID
	}

	var accepted, unavailable atomic.Int64
	var wg sync.WaitGroup
	for _, id := range tripIDs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.tripStore.Apply(f.ctx, owner, id, trips.Accept, d.ID)
			switch {
			case err == nil:
				accepted.Add(1)
			case errors.Is(err, ErrDriverUnavailable):
				unavailable.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(1), accepted.Load())
	assert.Equal(t, int64(n-1), unavailable.Load())
}

// two drivers race to accept the same trip: exactly one wins
func TestSameTripCannotBeAcceptedTwice(t *testing.T) {
	f := newFixture(t)
	owner := newOwner(t, f.db)
	d1 := f.availableDriver(t, owner, "+254700000001")
	d2 := f.availableDriver(t, owner, "+254700000002")
	tr := f.newTrip(t, owner)

	var won, refused atomic.Int64
	var wg sync.WaitGroup
	for _, d := range []Driver{d1, d2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.tripStore.Apply(f.ctx, owner, tr.ID, trips.Accept, d.ID)
			var te *TransitionError
			switch {
			case err == nil:
				won.Add(1)
			case errors.As(err, &te):
				refused.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(1), won.Load())
	assert.Equal(t, int64(1), refused.Load())

	// exactly one driver was taken, the loser stayed free
	busy := 0
	for _, d := range []Driver{d1, d2} {
		if f.driverStatus(t, owner, d.ID) == "on_trip" {
			busy++
		}
	}
	assert.Equal(t, 1, busy)
}
