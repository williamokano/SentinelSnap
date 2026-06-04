package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

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

type createSnapRequest struct {
	Latitude  float64  `json:"latitude"`
	Longitude float64  `json:"longitude"`
	Photos    []string `json:"photos"`
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

	for i, data := range req.Photos {
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			rollback()
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid base64 for photo %d: %s", i, err))
			return
		}

		ct := http.DetectContentType(raw)
		key := fmt.Sprintf("snaps/%d/%s%s", snapID, randomID(), extForContentType(ct))

		if _, err := h.storage.Put(ctx, key, bytes.NewReader(raw), ct); err != nil {
			rollback()
			writeError(w, http.StatusInternalServerError, "could not store photo")
			return
		}
		storedKeys = append(storedKeys, key)

		photoID, err := h.repo.AddPhoto(ctx, &domain.Photo{
			SnapID:    snapID,
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
			URL:       h.storage.URL(key),
			StoredKey: key,
		})
	}

	writeJSON(w, http.StatusCreated, domain.Snap{
		ID:        snapID,
		Latitude:  req.Latitude,
		Longitude: req.Longitude,
		Photos:    photos,
	})
}

func (h *SnapHandler) ListSnaps(w http.ResponseWriter, r *http.Request) {
	snaps, err := h.repo.ListSnaps(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list snaps")
		return
	}
	for i := range snaps {
		for j := range snaps[i].Photos {
			snaps[i].Photos[j].URL = h.storage.URL(snaps[i].Photos[j].StoredKey)
		}
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
