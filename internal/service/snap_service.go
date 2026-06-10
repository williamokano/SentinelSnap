// Package service owns SentinelSnap's business logic: input validation,
// photo decoding and storage, repository orchestration, metrics, and SSE
// broadcasts. Handlers stay thin: decode request, call the service, map
// errors, encode the response.
package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/williamokano/sentinelsnap/internal/domain"
	"github.com/williamokano/sentinelsnap/internal/hub"
	"github.com/williamokano/sentinelsnap/internal/observability"
	"github.com/williamokano/sentinelsnap/internal/repository"
	"github.com/williamokano/sentinelsnap/internal/storage"
)

// ValidationError reports invalid caller input. Handlers map it to a 400
// response carrying Msg verbatim.
type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

// sniffLen mirrors http.DetectContentType, which considers at most the first
// 512 bytes of data.
const sniffLen = 512

const maxSnapNameLen = 100

// maxPhotosPerSnap caps how many photos a single snap may carry; enforced
// here so every caller of the service gets the same rule, not just HTTP.
const maxPhotosPerSnap = 10

type SnapService struct {
	repo    repository.SnapRepository
	storage storage.StorageProvider
	hub     *hub.Hub
	metrics *observability.AppMetrics
}

func NewSnapService(repo repository.SnapRepository, store storage.StorageProvider, h *hub.Hub, metrics *observability.AppMetrics) *SnapService {
	return &SnapService{repo: repo, storage: store, hub: h, metrics: metrics}
}

// CreateSnapInput carries the validated-by-shape (but not yet by value)
// fields of a snap creation request. Photos are standard base64 payloads.
type CreateSnapInput struct {
	Latitude  float64
	Longitude float64
	Photos    []string
}

// CreateSnap validates the input, decodes and stores every photo, then
// persists the snap and its photo rows in a single transaction. Files are
// written to storage first; if the transaction fails they are deleted
// best-effort so storage does not accumulate orphans.
func (s *SnapService) CreateSnap(ctx context.Context, in CreateSnapInput) (*domain.Snap, error) {
	if in.Latitude < -90 || in.Latitude > 90 {
		return nil, &ValidationError{Msg: fmt.Sprintf("latitude must be between -90 and 90, got %g", in.Latitude)}
	}
	if in.Longitude < -180 || in.Longitude > 180 {
		return nil, &ValidationError{Msg: fmt.Sprintf("longitude must be between -180 and 180, got %g", in.Longitude)}
	}
	if len(in.Photos) == 0 {
		return nil, &ValidationError{Msg: "at least one photo is required"}
	}
	if len(in.Photos) > maxPhotosPerSnap {
		return nil, &ValidationError{Msg: fmt.Sprintf("at most %d photos per snap, got %d", maxPhotosPerSnap, len(in.Photos))}
	}

	var storedKeys []string
	cleanup := func() {
		// Cleanup must run even when the request context is already
		// canceled (e.g. the client gave up mid-upload).
		bg := context.WithoutCancel(ctx)
		for _, key := range storedKeys {
			if err := s.storage.Delete(bg, key); err != nil {
				slog.ErrorContext(bg, "cleanup: failed to delete stored photo", "key", key, "error", err)
			}
		}
	}

	photos := make([]domain.Photo, 0, len(in.Photos))
	for i, data := range in.Photos {
		photo, err := s.storePhoto(ctx, i, data)
		if err != nil {
			cleanup()
			return nil, err
		}
		storedKeys = append(storedKeys, photo.StoredKey)
		photos = append(photos, *photo)
	}

	snap := &domain.Snap{
		Latitude:  in.Latitude,
		Longitude: in.Longitude,
	}
	if err := s.repo.CreateSnapWithPhotos(ctx, snap, photos); err != nil {
		cleanup()
		return nil, fmt.Errorf("create snap with photos: %w", err)
	}

	for i := range photos {
		photos[i].URL = photoURL(photos[i].Token)
	}
	snap.Photos = photos

	s.metrics.SnapCreated(ctx)
	s.hub.Broadcast(ctx, hub.EventSnapCreated, snap)
	return snap, nil
}

// storePhoto streams one base64 photo payload into storage. The base64 input
// is decoded on the fly rather than buffered in full: only the first sniffLen
// bytes are materialized for content-type detection, and the remainder flows
// straight from the decoder into the storage backend.
func (s *SnapService) storePhoto(ctx context.Context, idx int, data string) (*domain.Photo, error) {
	dec := base64.NewDecoder(base64.StdEncoding, strings.NewReader(data))

	// Tiny images legitimately decode to fewer than sniffLen bytes, so a
	// short read is not an error here.
	head := make([]byte, sniffLen)
	n, err := io.ReadFull(dec, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		if isCorruptBase64(err) {
			return nil, &ValidationError{Msg: fmt.Sprintf("invalid base64 for photo %d: %s", idx, err)}
		}
		return nil, fmt.Errorf("read photo %d: %w", idx, err)
	}
	head = head[:n]

	ct := http.DetectContentType(head)
	ext := extForContentType(ct)
	if ext == "" {
		return nil, &ValidationError{Msg: fmt.Sprintf("unsupported content type %q for photo %d", ct, idx)}
	}

	token := randomID()
	key := fmt.Sprintf("photos/%s%s", token, ext)

	// Count bytes as they stream through Put: with streaming decode the total
	// size is only known once the copy completes.
	cr := &countingReader{r: io.MultiReader(bytes.NewReader(head), dec)}
	if err := s.storage.Put(ctx, key, cr, ct); err != nil {
		// Put may have created a partial file before failing; remove it
		// best-effort, even if the request context is already canceled.
		bg := context.WithoutCancel(ctx)
		if delErr := s.storage.Delete(bg, key); delErr != nil {
			slog.ErrorContext(bg, "cleanup: failed to delete partial photo", "key", key, "error", delErr)
		}
		// Invalid base64 surfaces mid-stream as the storage backend drains
		// the decoder, so a Put failure can be the caller's fault.
		if isCorruptBase64(err) {
			return nil, &ValidationError{Msg: fmt.Sprintf("invalid base64 for photo %d: %s", idx, err)}
		}
		return nil, fmt.Errorf("store photo %d: %w", idx, err)
	}

	s.metrics.PhotoStored(ctx, cr.n)

	return &domain.Photo{
		Token:     token,
		StoredKey: key,
	}, nil
}

// RenameSnap validates and persists a new name, then broadcasts the change.
func (s *SnapService) RenameSnap(ctx context.Context, id int64, name string) error {
	if len(name) > maxSnapNameLen {
		return &ValidationError{Msg: fmt.Sprintf("name must be %d characters or fewer", maxSnapNameLen)}
	}
	if err := s.repo.UpdateSnapName(ctx, id, name); err != nil {
		return err
	}
	s.hub.Broadcast(ctx, hub.EventSnapUpdated, map[string]any{"id": id, "name": name})
	return nil
}

// DeleteSnap removes the snap row first — the database is the source of
// truth, and the cascade removes the photo rows — then deletes the stored
// files best-effort. A failed file delete leaves an orphan file (logged),
// never a dangling DB row pointing at missing data.
func (s *SnapService) DeleteSnap(ctx context.Context, id int64) error {
	snap, err := s.repo.GetSnapByID(ctx, id)
	if err != nil {
		return err
	}

	if err := s.repo.DeleteSnap(ctx, id); err != nil {
		return fmt.Errorf("delete snap %d: %w", id, err)
	}

	for _, p := range snap.Photos {
		if err := s.storage.Delete(ctx, p.StoredKey); err != nil {
			slog.WarnContext(ctx, "failed to delete stored file", "key", p.StoredKey, "snap_id", id, "error", err)
		}
	}

	s.hub.Broadcast(ctx, hub.EventSnapDeleted, map[string]int64{"id": id})
	return nil
}

// ListSnaps returns all snaps with photo URLs populated.
func (s *SnapService) ListSnaps(ctx context.Context) ([]domain.Snap, error) {
	snaps, err := s.repo.ListSnaps(ctx)
	if err != nil {
		return nil, err
	}
	for i := range snaps {
		for j := range snaps[i].Photos {
			snaps[i].Photos[j].URL = photoURL(snaps[i].Photos[j].Token)
		}
	}
	return snaps, nil
}

// GetPhoto resolves a photo token and opens its stored file. The caller owns
// closing the returned reader.
func (s *SnapService) GetPhoto(ctx context.Context, token string) (io.ReadCloser, string, error) {
	photo, err := s.repo.GetPhotoByToken(ctx, token)
	if err != nil {
		return nil, "", err
	}
	rc, ct, err := s.storage.Get(ctx, photo.StoredKey)
	if err != nil {
		return nil, "", fmt.Errorf("read photo %q: %w", token, err)
	}
	return rc, ct, nil
}

func photoURL(token string) string {
	return "/photos/" + token
}

// isCorruptBase64 reports whether err stems from malformed base64 input.
// Storage backends wrap the copy error, so unwrap with errors.As.
func isCorruptBase64(err error) bool {
	var cie base64.CorruptInputError
	return errors.As(err, &cie)
}

// countingReader counts the bytes read through it so streaming uploads can
// report their size to metrics after the fact.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
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
