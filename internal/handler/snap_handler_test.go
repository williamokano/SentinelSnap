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
	storageMock "github.com/williamokano/sentinelsnap/internal/storage/mock"
)

func newHandler(repo *repoMock.SnapRepository, store *storageMock.StorageProvider) *handler.SnapHandler {
	return handler.NewSnapHandler(repo, store, hub.New(nil), &config.Config{}, nil)
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func postBody(lat, lng float64, photos []string) []byte {
	body := map[string]any{"latitude": lat, "longitude": lng, "photos": photos}
	b, _ := json.Marshal(body)
	return b
}

func postBodyRaw(raw map[string]any) []byte {
	b, _ := json.Marshal(raw)
	return b
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

	repo.On("CreateSnap", mock.Anything, mock.Anything).Return(int64(1), nil)
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	repo.On("AddPhoto", mock.Anything, mock.Anything).Return(int64(1), nil).Once()

	body := postBodyRaw(map[string]any{
		"latitude":  "52.5200",
		"longitude": "13.4050",
		"photos":    []string{b64("imgdata")},
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

	repo.On("CreateSnap", mock.Anything, mock.Anything).Return(int64(1), nil)
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	repo.On("AddPhoto", mock.Anything, mock.Anything).Return(int64(1), nil).Once()

	body := postBodyRaw(map[string]any{
		"latitude":  52.5200,
		"longitude": 13.4050,
		"photos":    []string{b64("imgdata")},
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
		"photos":    []string{b64("imgdata")},
	})
	req := httptest.NewRequest(http.MethodPost, "/snaps", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).CreateSnap(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	repo.AssertNotCalled(t, "CreateSnap")
}

func TestCreateSnap_Success(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("CreateSnap", mock.Anything, mock.Anything).Return(int64(7), nil)
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
	repo.On("AddPhoto", mock.Anything, mock.Anything).Return(int64(1), nil).Once()
	repo.On("AddPhoto", mock.Anything, mock.Anything).Return(int64(2), nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/snaps", bytes.NewReader(postBody(37.77, -122.41, []string{b64("imgdata1"), b64("imgdata2")})))
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

func TestCreateSnap_MissingLatLng(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	req := httptest.NewRequest(http.MethodPost, "/snaps", bytes.NewReader(postBody(0, 0, []string{b64("x")})))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).CreateSnap(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	repo.AssertNotCalled(t, "CreateSnap")
}

func TestCreateSnap_NoPhotos(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	req := httptest.NewRequest(http.MethodPost, "/snaps", bytes.NewReader(postBody(37.77, -122.41, nil)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).CreateSnap(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	repo.AssertNotCalled(t, "CreateSnap")
}

func TestCreateSnap_StorageFailure_Rollback(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("CreateSnap", mock.Anything, mock.Anything).Return(int64(7), nil)
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	repo.On("AddPhoto", mock.Anything, mock.Anything).Return(int64(1), nil).Once()
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError).Once()
	store.On("Delete", mock.Anything, mock.Anything).Return(nil).Once()
	repo.On("DeleteSnap", mock.Anything, int64(7)).Return(nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/snaps", bytes.NewReader(postBody(37.77, -122.41, []string{b64("img1"), b64("img2")})))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	newHandler(repo, store).CreateSnap(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	repo.AssertExpectations(t)
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
	// First delete succeeds, second fails — handler should continue and still delete the snap row.
	store.On("Delete", mock.Anything, "photos/ok.jpg").Return(nil).Once()
	store.On("Delete", mock.Anything, "photos/fail.jpg").Return(assert.AnError).Once()
	repo.On("DeleteSnap", mock.Anything, int64(5)).Return(nil).Once()

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
			Photos: []domain.Photo{{ID: 10, SnapID: 1, Token: "abc123", StoredKey: "snaps/1/p.jpg"}},
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
