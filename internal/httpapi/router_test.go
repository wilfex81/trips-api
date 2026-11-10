package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func TestProbes(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		dbErr  error
		status int
		body   string
	}{
		{"liveness", "/healthz", nil, 200, `{"status":"ok"}`},
		{"liveness ignores the database", "/healthz", errors.New("down"), 200, `{"status":"ok"}`},
		{"ready", "/readyz", nil, 200, `{"status":"ready"}`},
		{"not ready", "/readyz", errors.New("down"), 503, `{"status":"database unavailable"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			NewRouter(fakeDB{tc.dbErr}, nil).ServeHTTP(rec, req)

			assert.Equal(t, tc.status, rec.Code)
			assert.JSONEq(t, tc.body, rec.Body.String())
		})
	}
}
