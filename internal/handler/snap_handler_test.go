package handler_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/williamokano/sentinelsnap/internal/config"
	"github.com/williamokano/sentinelsnap/internal/domain"
	"github.com/williamokano/sentinelsnap/internal/handler"
	"github.com/williamokano/sentinelsnap/internal/hub"
	repoMock "github.com/williamokano/sentinelsnap/internal/repository/mock"
	"github.com/williamokano/sentinelsnap/internal/service"
	storageMock "github.com/williamokano/sentinelsnap/internal/storage/mock"
)

func newHandler(repo *repoMock.SnapRepository, store *storageMock.StorageProvider) *handler.SnapHandler {
	svc := service.NewSnapService(repo, store, hub.New(nil), nil)
	return handler.NewSnapHandler(svc, &config.Config{})
}

// pngB64 returns the base64 encoding of a minimal payload that
// http.DetectContentType sniffs as image/png. The filler just makes payloads
// distinguishable.
func pngB64(filler string) string {
	data := append([]byte("\x89PNG\r\n\x1a\n"), []byte(filler)...)
	return base64.StdEncoding.EncodeToString(data)
}

func postBody(lat, lng float64, photos []string) []byte {
	body := map[string]any{"latitude": lat, "longitude": lng, "photos": photos}
	b, _ := json.Marshal(body)
	return b
}

func postBodyRaw(raw map[string]any) []byte {
	b, _ := json.Marshal(raw)
	return b
}

// expectCreateSnapWithPhotos wires the transactional create mock, populating
// IDs back onto the snap and photo structs the way the real repo does.
func expectCreateSnapWithPhotos(repo *repoMock.SnapRepository, snapID int64) *mock.Call {
	return repo.On("CreateSnapWithPhotos", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			snap := args.Get(1).(*domain.Snap)
			snap.ID = snapID
			photos := args.Get(2).([]domain.Photo)
			for i := range photos {
				photos[i].ID = int64(i + 1)
				sid := snapID
				photos[i].SnapID = &sid
			}
		}).
		Return(nil)
}

// withParam injects a chi URL parameter into the request context.
func withParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestCreateSnap_LatLngAsStrings(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	expectCreateSnapWithPhotos(repo, 1)
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	body := postBodyRaw(map[string]any{
		"latitude":  "52.5200",
		"longitude": "13.4050",
		"photos":    []string{pngB64("imgdata")},
	})
	req := httptest.NewRequest(http.MethodPost, "/snaps", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).CreateSnap(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.InDelta(t, 52.52, resp["latitude"], 0.0001)
	assert.InDelta(t, 13.405, resp["longitude"], 0.0001)
}

func TestCreateSnap_LatLngAsNumbers(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	expectCreateSnapWithPhotos(repo, 1)
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	body := postBodyRaw(map[string]any{
		"latitude":  52.5200,
		"longitude": 13.4050,
		"photos":    []string{pngB64("imgdata")},
	})
	req := httptest.NewRequest(http.MethodPost, "/snaps", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).CreateSnap(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.InDelta(t, 52.52, resp["latitude"], 0.0001)
	assert.InDelta(t, 13.405, resp["longitude"], 0.0001)
}

func TestCreateSnap_LatLngInvalidString(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	body := postBodyRaw(map[string]any{
		"latitude":  "not-a-number",
		"longitude": "13.4050",
		"photos":    []string{pngB64("imgdata")},
	})
	req := httptest.NewRequest(http.MethodPost, "/snaps", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).CreateSnap(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	repo.AssertNotCalled(t, "CreateSnapWithPhotos")
}

func TestCreateSnap_Success(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	expectCreateSnapWithPhotos(repo, 7).Once()
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()

	req := httptest.NewRequest(http.MethodPost, "/snaps", bytes.NewReader(postBody(37.77, -122.41, []string{pngB64("imgdata1"), pngB64("imgdata2")})))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).CreateSnap(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(7), resp["id"])
	photos := resp["photos"].([]any)
	assert.Len(t, photos, 2)
	for _, p := range photos {
		photo := p.(map[string]any)
		url, ok := photo["url"].(string)
		assert.True(t, ok)
		assert.Contains(t, url, "/photos/")
	}

	repo.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestCreateSnap_NullIslandIsValid(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	expectCreateSnapWithPhotos(repo, 1)
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/snaps", bytes.NewReader(postBody(0, 0, []string{pngB64("x")})))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).CreateSnap(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	repo.AssertExpectations(t)
}

func TestCreateSnap_CoordinatesOutOfRange(t *testing.T) {
	tests := []struct {
		name     string
		lat, lng float64
	}{
		{"latitude above 90", 90.01, 13.4},
		{"latitude below -90", -91, 13.4},
		{"longitude above 180", 52.5, 180.5},
		{"longitude below -180", 52.5, -181},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &repoMock.SnapRepository{}
			store := &storageMock.StorageProvider{}

			req := httptest.NewRequest(http.MethodPost, "/snaps", bytes.NewReader(postBody(tc.lat, tc.lng, []string{pngB64("x")})))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			newHandler(repo, store).CreateSnap(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			repo.AssertNotCalled(t, "CreateSnapWithPhotos")
			store.AssertNotCalled(t, "Put")
		})
	}
}

func TestCreateSnap_NoPhotos(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	req := httptest.NewRequest(http.MethodPost, "/snaps", bytes.NewReader(postBody(37.77, -122.41, nil)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).CreateSnap(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	repo.AssertNotCalled(t, "CreateSnapWithPhotos")
}

func TestCreateSnap_InvalidBase64(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	req := httptest.NewRequest(http.MethodPost, "/snaps", bytes.NewReader(postBody(37.77, -122.41, []string{"!!!not-base64!!!"})))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).CreateSnap(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["error"], "photo 0")
	repo.AssertNotCalled(t, "CreateSnapWithPhotos")
}

func TestCreateSnap_UnsupportedContentType(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	plainText := base64.StdEncoding.EncodeToString([]byte("just some text, not an image"))
	req := httptest.NewRequest(http.MethodPost, "/snaps", bytes.NewReader(postBody(37.77, -122.41, []string{plainText})))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).CreateSnap(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["error"], "unsupported content type")
	repo.AssertNotCalled(t, "CreateSnapWithPhotos")
	store.AssertNotCalled(t, "Put")
}

func TestCreateSnap_BodyLimits(t *testing.T) {
	tooManyPhotos := make([]string, 11)
	for i := range tooManyPhotos {
		tooManyPhotos[i] = pngB64("imgdata")
	}
	// A single photo whose base64 payload pushes the body past the 50 MiB cap.
	oversized := postBody(37.77, -122.41, []string{strings.Repeat("A", 51<<20)})

	tests := []struct {
		name       string
		body       []byte
		wantStatus int
	}{
		{"body over 50 MiB returns 413", oversized, http.StatusRequestEntityTooLarge},
		{"more than 10 photos returns 400", postBody(37.77, -122.41, tooManyPhotos), http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &repoMock.SnapRepository{}
			store := &storageMock.StorageProvider{}

			req := httptest.NewRequest(http.MethodPost, "/snaps", bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			newHandler(repo, store).CreateSnap(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)
			repo.AssertNotCalled(t, "CreateSnapWithPhotos")
		})
	}
}

func TestUpdateSnap_BodyTooLarge(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	body := []byte(`{"name":"` + strings.Repeat("a", 2<<20) + `"}`)
	req := withParam(httptest.NewRequest(http.MethodPatch, "/snaps/1", bytes.NewReader(body)), "id", "1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).UpdateSnap(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	repo.AssertNotCalled(t, "UpdateSnapName")
}

func TestCreateSnap_StorageFailure_CleansUpFiles(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	// First photo stores fine; the second Put fails. The service deletes the
	// (possibly partial) file of the failed Put plus the already-stored file,
	// and never touches the database.
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError).Once()
	store.On("Delete", mock.Anything, mock.Anything).Return(nil).Twice()

	req := httptest.NewRequest(http.MethodPost, "/snaps", bytes.NewReader(postBody(37.77, -122.41, []string{pngB64("img1"), pngB64("img2")})))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).CreateSnap(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	repo.AssertNotCalled(t, "CreateSnapWithPhotos")
	store.AssertExpectations(t)
}

// ---- ServePhoto ----

func TestServePhoto_NotFound(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("GetPhotoByToken", mock.Anything, "deadbeef").Return(nil, domain.ErrNotFound)

	req := withParam(httptest.NewRequest(http.MethodGet, "/photos/deadbeef", nil), "token", "deadbeef")
	w := httptest.NewRecorder()

	newHandler(repo, store).ServePhoto(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	repo.AssertExpectations(t)
}

func TestServePhoto_StorageReadError(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	photo := &domain.Photo{ID: 1, Token: "abc123", StoredKey: "photos/abc123.jpg"}
	repo.On("GetPhotoByToken", mock.Anything, "abc123").Return(photo, nil)
	store.On("Get", mock.Anything, "photos/abc123.jpg").Return(nil, "", assert.AnError)

	req := withParam(httptest.NewRequest(http.MethodGet, "/photos/abc123", nil), "token", "abc123")
	w := httptest.NewRecorder()

	newHandler(repo, store).ServePhoto(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	repo.AssertExpectations(t)
	store.AssertExpectations(t)
}

// ---- UpdateSnap ----

func TestUpdateSnap_NameTooLong(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	longName := strings.Repeat("x", 101)
	body, _ := json.Marshal(map[string]string{"name": longName})
	req := withParam(httptest.NewRequest(http.MethodPatch, "/snaps/1", bytes.NewReader(body)), "id", "1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).UpdateSnap(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	repo.AssertNotCalled(t, "UpdateSnapName")
}

func TestUpdateSnap_NotFound(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("UpdateSnapName", mock.Anything, int64(99), "new name").Return(domain.ErrNotFound)

	body, _ := json.Marshal(map[string]string{"name": "new name"})
	req := withParam(httptest.NewRequest(http.MethodPatch, "/snaps/99", bytes.NewReader(body)), "id", "99")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).UpdateSnap(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	repo.AssertExpectations(t)
}

func TestUpdateSnap_RepoError(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("UpdateSnapName", mock.Anything, int64(1), "a name").Return(assert.AnError)

	body, _ := json.Marshal(map[string]string{"name": "a name"})
	req := withParam(httptest.NewRequest(http.MethodPatch, "/snaps/1", bytes.NewReader(body)), "id", "1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).UpdateSnap(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	repo.AssertExpectations(t)
}

// ---- DeleteSnap ----

func TestDeleteSnap_NotFound(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("GetSnapByID", mock.Anything, int64(42)).Return(nil, domain.ErrNotFound)

	req := withParam(httptest.NewRequest(http.MethodDelete, "/snaps/42", nil), "id", "42")
	w := httptest.NewRecorder()

	newHandler(repo, store).DeleteSnap(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	repo.AssertExpectations(t)
}

func TestDeleteSnap_PartialStorageFailureContinues(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	snap := &domain.Snap{
		ID: 5,
		Photos: []domain.Photo{
			{StoredKey: "photos/ok.jpg"},
			{StoredKey: "photos/fail.jpg"},
		},
	}
	repo.On("GetSnapByID", mock.Anything, int64(5)).Return(snap, nil)
	repo.On("DeleteSnap", mock.Anything, int64(5)).Return(nil).Once()
	// First delete succeeds, second fails — storage deletes are best-effort,
	// so the request still succeeds.
	store.On("Delete", mock.Anything, "photos/ok.jpg").Return(nil).Once()
	store.On("Delete", mock.Anything, "photos/fail.jpg").Return(assert.AnError).Once()

	req := withParam(httptest.NewRequest(http.MethodDelete, "/snaps/5", nil), "id", "5")
	w := httptest.NewRecorder()

	newHandler(repo, store).DeleteSnap(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	repo.AssertExpectations(t)
	store.AssertExpectations(t)
}

// ---- ListSnaps ----

func TestListSnaps_Empty(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("ListSnaps", mock.Anything).Return([]domain.Snap{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/snaps", nil)
	w := httptest.NewRecorder()

	newHandler(repo, store).ListSnaps(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp []any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp)
}

func TestListSnaps_WithData(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	snaps := []domain.Snap{
		{
			ID: 1, Latitude: 37.77, Longitude: -122.41,
			Photos: []domain.Photo{{ID: 10, Token: "abc123", StoredKey: "snaps/1/p.jpg"}},
		},
	}
	repo.On("ListSnaps", mock.Anything).Return(snaps, nil)

	req := httptest.NewRequest(http.MethodGet, "/snaps", nil)
	w := httptest.NewRecorder()

	newHandler(repo, store).ListSnaps(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp, 1)
	assert.Equal(t, float64(1), resp[0]["id"])
	photos := resp[0]["photos"].([]any)
	assert.Len(t, photos, 1)
	assert.Equal(t, "/photos/abc123", photos[0].(map[string]any)["url"])
}

// ---- CreatePhotos ----

func TestCreatePhotos_Success(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
	repo.On("CreatePhotos", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			photos := args.Get(1).([]domain.Photo)
			for i := range photos {
				photos[i].ID = int64(i + 1)
			}
		}).
		Return(nil).Once()

	body, _ := json.Marshal(map[string]any{"photos": []string{pngB64("a"), pngB64("b")}})
	req := httptest.NewRequest(http.MethodPost, "/photos", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).CreatePhotos(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp, 2)
	for _, p := range resp {
		url, ok := p["url"].(string)
		assert.True(t, ok)
		assert.Contains(t, url, "/photos/")
		_, hasSnapID := p["snap_id"]
		assert.False(t, hasSnapID, "standalone photos omit snap_id")
	}
	repo.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestCreatePhotos_NoPhotos(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	body, _ := json.Marshal(map[string]any{"photos": []string{}})
	req := httptest.NewRequest(http.MethodPost, "/photos", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).CreatePhotos(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	repo.AssertNotCalled(t, "CreatePhotos")
}

// ---- ListPhotos ----

func TestListPhotos_WithData(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("ListAllPhotos", mock.Anything).Return([]domain.Photo{
		{ID: 2, Token: "newer"}, {ID: 1, Token: "older"},
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/photos", nil)
	w := httptest.NewRecorder()

	newHandler(repo, store).ListPhotos(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp, 2)
	assert.Equal(t, "/photos/newer", resp[0]["url"])
	assert.Equal(t, "/photos/older", resp[1]["url"])
}

// ---- DeletePhoto ----

func TestDeletePhoto_Success(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("GetPhotoByToken", mock.Anything, "tok").
		Return(&domain.Photo{ID: 3, Token: "tok", StoredKey: "photos/tok.png"}, nil)
	repo.On("DeletePhoto", mock.Anything, int64(3)).Return(nil).Once()
	store.On("Delete", mock.Anything, "photos/tok.png").Return(nil).Once()

	req := withParam(httptest.NewRequest(http.MethodDelete, "/photos/tok", nil), "token", "tok")
	w := httptest.NewRecorder()

	newHandler(repo, store).DeletePhoto(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	repo.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestDeletePhoto_NotFound(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("GetPhotoByToken", mock.Anything, "nope").Return(nil, domain.ErrNotFound)

	req := withParam(httptest.NewRequest(http.MethodDelete, "/photos/nope", nil), "token", "nope")
	w := httptest.NewRecorder()

	newHandler(repo, store).DeletePhoto(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	repo.AssertExpectations(t)
}

func TestListSnaps_RepoError(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("ListSnaps", mock.Anything).Return([]domain.Snap(nil), assert.AnError)

	req := httptest.NewRequest(http.MethodGet, "/snaps", nil)
	w := httptest.NewRecorder()

	newHandler(repo, store).ListSnaps(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	repo.AssertExpectations(t)
}
