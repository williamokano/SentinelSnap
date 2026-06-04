package handler_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/williamokano/sentinelsnap/internal/config"
	"github.com/williamokano/sentinelsnap/internal/domain"
	"github.com/williamokano/sentinelsnap/internal/handler"
	repoMock "github.com/williamokano/sentinelsnap/internal/repository/mock"
	storageMock "github.com/williamokano/sentinelsnap/internal/storage/mock"
)

func newHandler(repo *repoMock.SnapRepository, store *storageMock.StorageProvider) *handler.SnapHandler {
	return handler.NewSnapHandler(repo, store, &config.Config{})
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

func TestCreateSnap_LatLngAsStrings(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("CreateSnap", mock.Anything, mock.Anything).Return(int64(1), nil)
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("", nil).Once()
	store.On("URL", mock.Anything).Return("/uploads/snaps/1/abc.jpg").Once()
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
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("", nil).Once()
	store.On("URL", mock.Anything).Return("/uploads/snaps/1/abc.jpg").Once()
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
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("", nil).Twice()
	store.On("URL", mock.Anything).Return("/uploads/snaps/7/abc.jpg").Twice()
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
	assert.Len(t, resp["photos"], 2)

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
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("", nil).Once()
	store.On("URL", mock.Anything).Return("/uploads/snaps/7/abc.jpg").Once()
	repo.On("AddPhoto", mock.Anything, mock.Anything).Return(int64(1), nil).Once()
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("", assert.AnError).Once()
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
			Photos: []domain.Photo{{ID: 10, SnapID: 1, StoredKey: "snaps/1/p.jpg"}},
		},
	}
	repo.On("ListSnaps", mock.Anything).Return(snaps, nil)
	store.On("URL", "snaps/1/p.jpg").Return("/uploads/snaps/1/p.jpg").Once()

	req := httptest.NewRequest(http.MethodGet, "/snaps", nil)
	w := httptest.NewRecorder()

	newHandler(repo, store).ListSnaps(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp, 1)
	assert.Equal(t, float64(1), resp[0]["id"])
	assert.Len(t, resp[0]["photos"], 1)
}
