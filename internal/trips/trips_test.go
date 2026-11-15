package trips

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The expected table is written out by hand, not read from `rules`,
// so the test checks the specification and not the implementation.
func TestEveryCombination(t *testing.T) {
	statuses := []Status{Requested, Accepted, InProgress, Completed, Cancelled}
	actions := []Action{Accept, Start, Complete, Cancel}

	allowed := map[[2]string]Status{
		{"requested", "accept"}:     Accepted,
		{"accepted", "start"}:       InProgress,
		{"in_progress", "complete"}: Completed,
		{"requested", "cancel"}:     Cancelled,
		{"accepted", "cancel"}:      Cancelled,
	}

	for _, from := range statuses {
		for _, a := range actions {
			want, ok := allowed[[2]string{string(from), string(a)}]
			got, err := Next(from, a)
			if ok {
				assert.NoError(t, err, "%s + %s", from, a)
				assert.Equal(t, want, got, "%s + %s", from, a)
			} else {
				assert.ErrorIs(t, err, ErrInvalidTransition, "%s + %s", from, a)
				assert.Empty(t, got, "%s + %s", from, a)
			}
		}
	}
}

func TestFinishedTripsAreFrozen(t *testing.T) {
	for _, from := range []Status{Completed, Cancelled} {
		for _, a := range []Action{Accept, Start, Complete, Cancel} {
			_, err := Next(from, a)
			assert.ErrorIs(t, err, ErrInvalidTransition, "%s + %s", from, a)
		}
	}
}

func TestUnknownAction(t *testing.T) {
	_, err := Next(Requested, Action("teleport"))
	assert.ErrorIs(t, err, ErrInvalidTransition)
}

func TestDriverStatus(t *testing.T) {
	s, ok := DriverStatus(Accept)
	assert.True(t, ok)
	assert.Equal(t, "on_trip", s)

	for _, a := range []Action{Complete, Cancel} {
		s, ok := DriverStatus(a)
		assert.True(t, ok)
		assert.Equal(t, "available", s)
	}

	_, ok = DriverStatus(Start)
	assert.False(t, ok)
}

func TestStatusesMatchRules(t *testing.T) {
	assert.Len(t, Statuses, 5)
	assert.Contains(t, Statuses, string(InProgress))
}
