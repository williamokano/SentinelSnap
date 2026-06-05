package handler

import (
	"net/http"

	"github.com/jmoiron/sqlx"
)

// HealthHandler handles health check requests.
type HealthHandler struct {
	db *sqlx.DB
}

// NewHealthHandler creates a new HealthHandler with the given database connection.
func NewHealthHandler(db *sqlx.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

// Check handles GET /healthz. It pings the database and returns 200 with
// {"status":"ok"} if healthy, or 503 with {"status":"error","error":"..."} if not.
func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	if err := h.db.PingContext(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
