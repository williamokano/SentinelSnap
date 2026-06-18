package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/williamokano/sentinelsnap/internal/config"
	"github.com/williamokano/sentinelsnap/internal/domain"
	"github.com/williamokano/sentinelsnap/internal/service"
)

const (
	// maxSnapBodyBytes caps POST /snaps bodies: the service's photo-count cap
	// worth of base64 photos plus JSON overhead fit comfortably within 50 MiB.
	maxSnapBodyBytes   = 50 << 20
	maxUpdateBodyBytes = 1 << 20
)

type SnapHandler struct {
	svc *service.SnapService
	cfg *config.Config
}

func NewSnapHandler(svc *service.SnapService, cfg *config.Config) *SnapHandler {
	return &SnapHandler{svc: svc, cfg: cfg}
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

// decodeJSONBody decodes the request body into v, translating the
// MaxBytesReader limit into a 413 and malformed JSON into a 400. It reports
// whether decoding succeeded; on failure the response has been written.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("request body exceeds %d bytes", maxBytesErr.Limit))
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

// writeServiceError maps service-layer errors onto HTTP responses:
// validation failures become 400s carrying the message, missing entities
// become 404s, and anything else is a 500 with the generic fallback message.
func writeServiceError(w http.ResponseWriter, err error, notFoundMsg, fallbackMsg string) {
	var ve *service.ValidationError
	switch {
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, ve.Msg)
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, notFoundMsg)
	default:
		writeError(w, http.StatusInternalServerError, fallbackMsg)
	}
}

func (h *SnapHandler) CreateSnap(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSnapBodyBytes)

	var req createSnapRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}

	snap, err := h.svc.CreateSnap(r.Context(), service.CreateSnapInput{
		Latitude:  float64(req.Latitude),
		Longitude: float64(req.Longitude),
		Photos:    req.Photos,
	})
	if err != nil {
		writeServiceError(w, err, "snap not found", "could not create snap")
		return
	}

	writeJSON(w, http.StatusCreated, snap)
}

type createPhotosRequest struct {
	Photos []string `json:"photos"`
}

func (h *SnapHandler) CreatePhotos(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSnapBodyBytes)

	var req createPhotosRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}

	photos, err := h.svc.CreatePhotos(r.Context(), req.Photos)
	if err != nil {
		writeServiceError(w, err, "photo not found", "could not create photos")
		return
	}

	writeJSON(w, http.StatusCreated, photos)
}

func (h *SnapHandler) ListPhotos(w http.ResponseWriter, r *http.Request) {
	photos, err := h.svc.ListAllPhotos(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list photos")
		return
	}
	writeJSON(w, http.StatusOK, photos)
}

func (h *SnapHandler) DeletePhoto(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if err := h.svc.DeletePhoto(r.Context(), token); err != nil {
		writeServiceError(w, err, "photo not found", "could not delete photo")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *SnapHandler) ServePhoto(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	rc, ct, err := h.svc.GetPhoto(r.Context(), token)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		writeError(w, http.StatusInternalServerError, "could not fetch photo")
		return
	}
	defer func() { _ = rc.Close() }()

	w.Header().Set("Content-Type", ct)
	if _, err := io.Copy(w, rc); err != nil {
		slog.WarnContext(r.Context(), "failed to stream photo", "token", token, "error", err)
	}
}

// ServePhotoThumb serves a JPEG thumbnail (max 400 px on the longest side)
// for the photo identified by {token}. Thumbnails are not counted as views.
func (h *SnapHandler) ServePhotoThumb(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	rc, err := h.svc.GetPhotoThumb(r.Context(), token, 400)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		writeError(w, http.StatusInternalServerError, "could not generate thumbnail")
		return
	}
	defer func() { _ = rc.Close() }()

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if _, err := io.Copy(w, rc); err != nil {
		slog.WarnContext(r.Context(), "failed to stream thumbnail", "token", token, "error", err)
	}
}

func (h *SnapHandler) UpdateSnap(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid snap id")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUpdateBodyBytes)

	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	if err := h.svc.RenameSnap(r.Context(), id, req.Name); err != nil {
		writeServiceError(w, err, "snap not found", "could not update snap")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": req.Name})
}

func (h *SnapHandler) DeleteSnap(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid snap id")
		return
	}

	if err := h.svc.DeleteSnap(r.Context(), id); err != nil {
		writeServiceError(w, err, "snap not found", "could not delete snap")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *SnapHandler) ListSnaps(w http.ResponseWriter, r *http.Request) {
	snaps, err := h.svc.ListSnaps(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list snaps")
		return
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
