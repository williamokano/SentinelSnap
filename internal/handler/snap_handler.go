package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/williamokano/sentinelsnap/internal/config"
	"github.com/williamokano/sentinelsnap/internal/domain"
	"github.com/williamokano/sentinelsnap/internal/hub"
	"github.com/williamokano/sentinelsnap/internal/observability"
	"github.com/williamokano/sentinelsnap/internal/repository"
	"github.com/williamokano/sentinelsnap/internal/storage"
)

type SnapHandler struct {
	repo    repository.SnapRepository
	storage storage.StorageProvider
	hub     *hub.Hub
	cfg     *config.Config
	metrics *observability.AppMetrics
}

func NewSnapHandler(repo repository.SnapRepository, store storage.StorageProvider, h *hub.Hub, cfg *config.Config, metrics *observability.AppMetrics) *SnapHandler {
	return &SnapHandler{repo: repo, storage: store, hub: h, cfg: cfg, metrics: metrics}
}

// flexFloat64 unmarshals from both JSON number and string,
// because iOS Shortcuts sends coordinates as strings.
type flexFloat64 float64

func (f *flexFloat64) UnmarshalJSON(data []byte) error {
	var n float64
	if err := json.Unmarshal(data, &n); err == nil {
		*f = flexFloat64(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = flexFloat64(n)
	return nil
}

type createSnapRequest struct {
	Latitude  flexFloat64 `json:"latitude"`
	Longitude flexFloat64 `json:"longitude"`
	Photos    []string    `json:"photos"`
}

func (h *SnapHandler) CreateSnap(w http.ResponseWriter, r *http.Request) {
	var req createSnapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Latitude == 0 && req.Longitude == 0 {
		writeError(w, http.StatusBadRequest, "latitude and longitude are required")
		return
	}
	if len(req.Photos) == 0 {
		writeError(w, http.StatusBadRequest, "at least one photo is required")
		return
	}

	ctx := r.Context()

	snapID, err := h.repo.CreateSnap(ctx, &domain.Snap{
		Latitude:  float64(req.Latitude),
		Longitude: float64(req.Longitude),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create snap")
		return
	}

	var storedKeys []string
	var photos []domain.Photo

	rollback := func() {
		bg := context.Background()
		for _, key := range storedKeys {
			if err := h.storage.Delete(bg, key); err != nil {
				slog.ErrorContext(bg, "rollback: failed to delete stored photo",
					"key", key, "snap_id", snapID, "error", err)
			}
		}
		if err := h.repo.DeleteSnap(bg, snapID); err != nil {
			slog.ErrorContext(bg, "rollback: failed to delete snap record",
				"snap_id", snapID, "error", err)
		}
	}

	for i, data := range req.Photos {
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			rollback()
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid base64 for photo %d: %s", i, err))
			return
		}

		ct := http.DetectContentType(raw)
		token := randomID()
		key := fmt.Sprintf("photos/%s%s", token, extForContentType(ct))

		if err := h.storage.Put(ctx, key, bytes.NewReader(raw), ct); err != nil {
			rollback()
			writeError(w, http.StatusInternalServerError, "could not store photo")
			return
		}
		storedKeys = append(storedKeys, key)

		photoID, err := h.repo.AddPhoto(ctx, &domain.Photo{
			SnapID:    snapID,
			StoredKey: key,
			Token:     token,
		})
		if err != nil {
			rollback()
			writeError(w, http.StatusInternalServerError, "could not save photo metadata")
			return
		}

		h.metrics.PhotoStored(ctx, int64(len(raw)))

		photos = append(photos, domain.Photo{
			ID:        photoID,
			SnapID:    snapID,
			Token:     token,
			URL:       fmt.Sprintf("/photos/%s", token),
			StoredKey: key,
		})
	}

	snap := domain.Snap{
		ID:        snapID,
		Latitude:  float64(req.Latitude),
		Longitude: float64(req.Longitude),
		Photos:    photos,
	}
	h.hub.Broadcast(hub.EventSnapCreated, snap)
	writeJSON(w, http.StatusCreated, snap)
	h.metrics.SnapCreated(ctx)
}

func (h *SnapHandler) ServePhoto(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	photo, err := h.repo.GetPhotoByToken(r.Context(), token)
	if err != nil {
		if err == domain.ErrNotFound {
			http.NotFound(w, r)
			return
		}
		writeError(w, http.StatusInternalServerError, "could not fetch photo")
		return
	}

	rc, ct, err := h.storage.Get(r.Context(), photo.StoredKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read photo")
		return
	}
	defer func() { _ = rc.Close() }()

	w.Header().Set("Content-Type", ct)
	if _, err := io.Copy(w, rc); err != nil {
		slog.WarnContext(r.Context(), "failed to stream photo", "token", token, "error", err)
	}
}

func (h *SnapHandler) UpdateSnap(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid snap id")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(req.Name) > 100 {
		writeError(w, http.StatusBadRequest, "name must be 100 characters or fewer")
		return
	}

	if err := h.repo.UpdateSnapName(r.Context(), id, req.Name); err != nil {
		if err == domain.ErrNotFound {
			writeError(w, http.StatusNotFound, "snap not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not update snap")
		return
	}

	h.hub.Broadcast(hub.EventSnapUpdated, map[string]any{"id": id, "name": req.Name})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": req.Name})
}

func (h *SnapHandler) DeleteSnap(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid snap id")
		return
	}

	snap, err := h.repo.GetSnapByID(r.Context(), id)
	if err != nil {
		if err == domain.ErrNotFound {
			writeError(w, http.StatusNotFound, "snap not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not fetch snap")
		return
	}

	for _, p := range snap.Photos {
		if err := h.storage.Delete(r.Context(), p.StoredKey); err != nil {
			slog.WarnContext(r.Context(), "failed to delete stored file", "key", p.StoredKey, "error", err)
		}
	}

	if err := h.repo.DeleteSnap(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete snap")
		return
	}

	h.hub.Broadcast(hub.EventSnapDeleted, map[string]int64{"id": id})
	w.WriteHeader(http.StatusNoContent)
}

func (h *SnapHandler) ListSnaps(w http.ResponseWriter, r *http.Request) {
	snaps, err := h.repo.ListSnaps(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list snaps")
		return
	}
	for i := range snaps {
		for j := range snaps[i].Photos {
			snaps[i].Photos[j].URL = fmt.Sprintf("/photos/%s", snaps[i].Photos[j].Token)
		}
	}
	writeJSON(w, http.StatusOK, snaps)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writeJSON: failed to encode response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func extForContentType(ct string) string {
	switch ct {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}
