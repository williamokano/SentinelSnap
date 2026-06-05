package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/williamokano/sentinelsnap/internal/handler"
)

// stubPinger is a simple in-test implementation of DBPinger.
type stubPinger struct {
	err error
}

func (s *stubPinger) PingContext(_ context.Context) error {
	return s.err
}

func TestHealthCheck(t *testing.T) {
	tests := []struct {
		name       string
		pingerErr  error
		wantStatus int
		wantBody   map[string]string
	}{
		{
			name:       "healthy: ping returns nil → 200 ok",
			pingerErr:  nil,
			wantStatus: http.StatusOK,
			wantBody:   map[string]string{"status": "ok"},
		},
		{
			name:       "unhealthy: ping returns error → 503 with generic message",
			pingerErr:  errors.New("connection refused"),
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   map[string]string{"status": "error", "error": "database unavailable"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := handler.NewHealthHandler(&stubPinger{err: tc.pingerErr})

			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			w := httptest.NewRecorder()

			h.Check(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)

			var body map[string]string
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, tc.wantBody, body)
		})
	}
}
