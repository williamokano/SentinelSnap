package handler

import (
	"context"
	"net/http"
)

// DBPinger is a narrow interface for checking database connectivity.
type DBPinger interface {
	PingContext(ctx context.Context) error
}

// HealthHandler handles health check requests.
type HealthHandler struct {
	db DBPinger
}

// NewHealthHandler creates a new HealthHandler with the given database connection.
func NewHealthHandler(db DBPinger) *HealthHandler {
	return &HealthHandler{db: db}
}

// Check handles GET /healthz. It pings the database and returns 200 with
// {"status":"ok"} if healthy, or 503 with {"status":"error","error":"database unavailable"} if not.
func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	if err := h.db.PingContext(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "error",
			"error":  "database unavailable",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
