package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/williamokano/sentinelsnap/internal/config"
	"github.com/williamokano/sentinelsnap/internal/domain"
	"github.com/williamokano/sentinelsnap/internal/repository"
	"github.com/williamokano/sentinelsnap/internal/storage"
)

type SnapHandler struct {
	repo    repository.SnapRepository
	storage storage.StorageProvider
	cfg     *config.Config
}

func NewSnapHandler(repo repository.SnapRepository, store storage.StorageProvider, cfg *config.Config) *SnapHandler {
	return &SnapHandler{repo: repo, storage: store, cfg: cfg}
}

// createSnapRequest is the JSON body for POST /snaps.
type createSnapRequest struct {
	Latitude  float64       `json:"latitude"`
	Longitude float64       `json:"longitude"`
	Photos    []photoUpload `json:"photos"`
}

type photoUpload struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	// Data is the base64-encoded raw file content.
	Data string `json:"data"`
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
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create snap")
		return
	}

	var storedKeys []string
	var photos []domain.Photo

	rollback := func() {
		for _, key := range storedKeys {
			_ = h.storage.Delete(context.Background(), key)
		}
		_ = h.repo.DeleteSnap(context.Background(), snapID)
	}

	for _, up := range req.Photos {
		raw, err := base64.StdEncoding.DecodeString(up.Data)
		if err != nil {
			rollback()
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid base64 for %q: %s", up.Filename, err))
			return
		}

		filename := sanitizeFilename(up.Filename)
		key := fmt.Sprintf("snaps/%d/%d_%s", snapID, time.Now().UnixNano(), filename)

		ct := up.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}

		url, err := h.storage.Put(ctx, key, bytes.NewReader(raw), ct)
		if err != nil {
			rollback()
			writeError(w, http.StatusInternalServerError, "could not store photo")
			return
		}
		storedKeys = append(storedKeys, key)

		photoID, err := h.repo.AddPhoto(ctx, &domain.Photo{
			SnapID:    snapID,
			URL:       url,
			StoredKey: key,
		})
		if err != nil {
			rollback()
			writeError(w, http.StatusInternalServerError, "could not save photo metadata")
			return
		}

		photos = append(photos, domain.Photo{
			ID:        photoID,
			SnapID:    snapID,
			URL:       url,
			StoredKey: key,
		})
	}

	snap := domain.Snap{
		ID:        snapID,
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
		Photos:    photos,
	}
	writeJSON(w, http.StatusCreated, snap)
}

func (h *SnapHandler) ListSnaps(w http.ResponseWriter, r *http.Request) {
	snaps, err := h.repo.ListSnaps(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list snaps")
		return
	}
	writeJSON(w, http.StatusOK, snaps)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func sanitizeFilename(name string) string {
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
	if name == "" || name == "." {
		return "upload"
	}
	return name
}
