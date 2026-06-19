package service_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/williamokano/sentinelsnap/internal/domain"
	"github.com/williamokano/sentinelsnap/internal/hub"
	repoMock "github.com/williamokano/sentinelsnap/internal/repository/mock"
	"github.com/williamokano/sentinelsnap/internal/service"
	storageMock "github.com/williamokano/sentinelsnap/internal/storage/mock"
)

func newService(repo *repoMock.SnapRepository, store *storageMock.StorageProvider) *service.SnapService {
	return service.NewSnapService(repo, store, hub.New(nil), nil)
}

// pngB64 returns the base64 encoding of a payload that http.DetectContentType
// sniffs as image/png.
func pngB64(filler string) string {
	data := append([]byte("\x89PNG\r\n\x1a\n"), []byte(filler)...)
	return base64.StdEncoding.EncodeToString(data)
}

func validInput(photos ...string) service.CreateSnapInput {
	return service.CreateSnapInput{Latitude: 37.77, Longitude: -122.41, Photos: photos}
}

func assertValidationError(t *testing.T, err error, contains ...string) {
	t.Helper()
	var ve *service.ValidationError
	require.ErrorAs(t, err, &ve)
	for _, c := range contains {
		assert.Contains(t, ve.Msg, c)
	}
}

func TestCreateSnap_HappyPath(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	var putKeys []string
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, "image/png").
		Run(func(args mock.Arguments) {
			key := args.String(1)
			putKeys = append(putKeys, key)
			// Drain like a real backend so the streaming decode runs.
			_, err := io.Copy(io.Discard, args.Get(2).(io.Reader))
			require.NoError(t, err)
		}).
		Return(nil).Twice()

	repo.On("CreateSnapWithPhotos", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			snap := args.Get(1).(*domain.Snap)
			snap.ID = 42
			photos := args.Get(2).([]domain.Photo)
			for i := range photos {
				photos[i].ID = int64(i + 1)
				sid := int64(42)
				photos[i].SnapID = &sid
			}
		}).
		Return(nil).Once()

	snap, err := newService(repo, store).CreateSnap(context.Background(), validInput(pngB64("one"), pngB64("two")))

	require.NoError(t, err)
	assert.Equal(t, int64(42), snap.ID)
	assert.InDelta(t, 37.77, snap.Latitude, 0.0001)
	require.Len(t, snap.Photos, 2)
	for i, p := range snap.Photos {
		assert.Equal(t, int64(i+1), p.ID)
		require.NotNil(t, p.SnapID)
		assert.Equal(t, int64(42), *p.SnapID)
		assert.Equal(t, "/photos/"+p.Token, p.URL)
		assert.Equal(t, "photos/"+p.Token+".png", p.StoredKey)
		assert.Contains(t, putKeys, p.StoredKey)
	}

	repo.AssertExpectations(t)
	store.AssertExpectations(t)
	store.AssertNotCalled(t, "Delete")
}

func TestCreateSnap_CoordinateValidation(t *testing.T) {
	tests := []struct {
		name     string
		lat, lng float64
		wantMsg  string
	}{
		{"latitude too high", 90.5, 0, "latitude must be between -90 and 90"},
		{"latitude too low", -90.5, 0, "latitude must be between -90 and 90"},
		{"longitude too high", 0, 180.5, "longitude must be between -180 and 180"},
		{"longitude too low", 0, -180.5, "longitude must be between -180 and 180"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &repoMock.SnapRepository{}
			store := &storageMock.StorageProvider{}

			_, err := newService(repo, store).CreateSnap(context.Background(), service.CreateSnapInput{
				Latitude:  tc.lat,
				Longitude: tc.lng,
				Photos:    []string{pngB64("x")},
			})

			assertValidationError(t, err, tc.wantMsg)
			repo.AssertNotCalled(t, "CreateSnapWithPhotos")
			store.AssertNotCalled(t, "Put")
		})
	}
}

// A snap at coordinates 0,0 ("null island") is rejected: a GPS-less client that
// omits coordinates sends them as the zero value, and we refuse to create a junk
// snap pinned to the Atlantic. The request is turned away before any photo is
// stored or any row is written — such uploads belong on POST /photos instead.
func TestCreateSnap_NullIslandRejected(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	_, err := newService(repo, store).CreateSnap(context.Background(), service.CreateSnapInput{
		Latitude:  0,
		Longitude: 0,
		Photos:    []string{pngB64("x")},
	})

	assertValidationError(t, err, "latitude and longitude are required")
	store.AssertNotCalled(t, "Put")
	repo.AssertNotCalled(t, "CreateSnapWithPhotos")
}

func TestCreateSnap_NoPhotos(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	_, err := newService(repo, store).CreateSnap(context.Background(), validInput())

	assertValidationError(t, err, "at least one photo is required")
}

func TestCreateSnap_UnsupportedContentType(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	// First photo stores fine, second is rejected by content-type sniffing:
	// the error names the detected type and the offending photo's index, and
	// the already-stored first photo is cleaned up.
	plainText := base64.StdEncoding.EncodeToString([]byte("definitely not an image"))
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, "image/png").Return(nil).Once()
	store.On("Delete", mock.Anything, mock.Anything).Return(nil).Once()

	_, err := newService(repo, store).CreateSnap(context.Background(), validInput(pngB64("ok"), plainText))

	assertValidationError(t, err, "unsupported content type", "text/plain", "photo 1")
	repo.AssertNotCalled(t, "CreateSnapWithPhotos")
	store.AssertExpectations(t)
}

func TestCreateSnap_InvalidBase64InHead(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	_, err := newService(repo, store).CreateSnap(context.Background(), validInput("***garbage***"))

	assertValidationError(t, err, "invalid base64", "photo 0")
	store.AssertNotCalled(t, "Put")
	repo.AssertNotCalled(t, "CreateSnapWithPhotos")
}

// drainingStore behaves like a real storage backend: Put consumes the reader,
// so base64 corruption beyond the sniffed head surfaces as a (wrapped) Put
// error. Deletes are recorded for assertions.
type drainingStore struct {
	mu      sync.Mutex
	deleted []string
}

func (d *drainingStore) Put(_ context.Context, key string, r io.Reader, _ string) error {
	if _, err := io.Copy(io.Discard, r); err != nil {
		return fmt.Errorf("write file %q: %w", key, err)
	}
	return nil
}

func (d *drainingStore) Get(context.Context, string) (io.ReadCloser, string, error) {
	return nil, "", fmt.Errorf("not implemented")
}

func (d *drainingStore) Delete(_ context.Context, key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleted = append(d.deleted, key)
	return nil
}

func TestCreateSnap_InvalidBase64MidStream(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &drainingStore{}

	// Valid base64 for >512 decoded bytes (so the head sniff succeeds), then
	// a corrupt 4-char quantum that only the streaming copy inside Put
	// encounters.
	big := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 1024)...)
	corrupted := base64.StdEncoding.EncodeToString(big) + "!!!!"

	svc := service.NewSnapService(repo, store, hub.New(nil), nil)
	_, err := svc.CreateSnap(context.Background(), validInput(corrupted))

	assertValidationError(t, err, "invalid base64", "photo 0")
	// The partial file from the failed Put must be removed.
	assert.Len(t, store.deleted, 1)
	repo.AssertNotCalled(t, "CreateSnapWithPhotos")
}

func TestCreateSnap_TxFailureDeletesStoredFiles(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
	repo.On("CreateSnapWithPhotos", mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError).Once()
	// Both stored files are deleted best-effort after the transaction fails.
	store.On("Delete", mock.Anything, mock.Anything).Return(nil).Twice()

	_, err := newService(repo, store).CreateSnap(context.Background(), validInput(pngB64("one"), pngB64("two")))

	require.Error(t, err)
	var ve *service.ValidationError
	assert.False(t, errors.As(err, &ve), "a tx failure is not the caller's fault")
	repo.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestCreateSnap_StorageFailureCleansUpEarlierFiles(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError).Once()
	// One delete for the failed Put's partial file, one for the first photo.
	store.On("Delete", mock.Anything, mock.Anything).Return(nil).Twice()

	_, err := newService(repo, store).CreateSnap(context.Background(), validInput(pngB64("one"), pngB64("two")))

	require.Error(t, err)
	var ve *service.ValidationError
	assert.False(t, errors.As(err, &ve), "an infrastructure failure is not the caller's fault")
	repo.AssertNotCalled(t, "CreateSnapWithPhotos")
	store.AssertExpectations(t)
}

func TestRenameSnap_TooLong(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	err := newService(repo, store).RenameSnap(context.Background(), 1, strings.Repeat("x", 101))

	assertValidationError(t, err, "name must be 100 characters or fewer")
	repo.AssertNotCalled(t, "UpdateSnapName")
}

func TestRenameSnap_NotFoundPassesThrough(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("UpdateSnapName", mock.Anything, int64(9), "name").Return(domain.ErrNotFound)

	err := newService(repo, store).RenameSnap(context.Background(), 9, "name")

	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestDeleteSnap_DBRowDeletedBeforeStorage(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	var order []string
	snap := &domain.Snap{ID: 5, Photos: []domain.Photo{{StoredKey: "photos/a.jpg"}, {StoredKey: "photos/b.jpg"}}}
	repo.On("GetSnapByID", mock.Anything, int64(5)).Return(snap, nil)
	repo.On("DeleteSnap", mock.Anything, int64(5)).
		Run(func(mock.Arguments) { order = append(order, "db") }).
		Return(nil).Once()
	store.On("Delete", mock.Anything, mock.Anything).
		Run(func(mock.Arguments) { order = append(order, "storage") }).
		Return(nil).Twice()

	err := newService(repo, store).DeleteSnap(context.Background(), 5)

	require.NoError(t, err)
	require.Equal(t, []string{"db", "storage", "storage"}, order,
		"the DB row must be deleted before any storage files")
	repo.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestDeleteSnap_DBFailureLeavesStorageUntouched(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	snap := &domain.Snap{ID: 5, Photos: []domain.Photo{{StoredKey: "photos/a.jpg"}}}
	repo.On("GetSnapByID", mock.Anything, int64(5)).Return(snap, nil)
	repo.On("DeleteSnap", mock.Anything, int64(5)).Return(assert.AnError).Once()

	err := newService(repo, store).DeleteSnap(context.Background(), 5)

	require.Error(t, err)
	store.AssertNotCalled(t, "Delete")
}

func TestGetPhoto_NotFoundPassesThrough(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("GetPhotoByToken", mock.Anything, "nope").Return(nil, domain.ErrNotFound)

	_, _, err := newService(repo, store).GetPhoto(context.Background(), "nope")

	assert.ErrorIs(t, err, domain.ErrNotFound)
	store.AssertNotCalled(t, "Get")
}

func TestGetPhoto_Success(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	photo := &domain.Photo{Token: "abc", StoredKey: "photos/abc.jpg"}
	repo.On("GetPhotoByToken", mock.Anything, "abc").Return(photo, nil)
	store.On("Get", mock.Anything, "photos/abc.jpg").
		Return(io.NopCloser(strings.NewReader("bytes")), "image/jpeg", nil)
	repo.On("IncrementPhotoViews", mock.Anything, "abc").Return(nil)

	rc, ct, err := newService(repo, store).GetPhoto(context.Background(), "abc")

	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	assert.Equal(t, "image/jpeg", ct)
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, "bytes", string(data))
}

func TestCreatePhotos_HappyPath(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	store.On("Put", mock.Anything, mock.Anything, mock.Anything, "image/png").Return(nil).Twice()
	repo.On("CreatePhotos", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			photos := args.Get(1).([]domain.Photo)
			for i := range photos {
				photos[i].ID = int64(i + 1)
			}
		}).
		Return(nil).Once()

	photos, err := newService(repo, store).CreatePhotos(context.Background(), []string{pngB64("a"), pngB64("b")})

	require.NoError(t, err)
	require.Len(t, photos, 2)
	for i, p := range photos {
		assert.Equal(t, int64(i+1), p.ID)
		assert.Nil(t, p.SnapID, "standalone uploads have no snap")
		assert.Equal(t, "/photos/"+p.Token, p.URL)
	}
	repo.AssertExpectations(t)
	store.AssertExpectations(t)
	store.AssertNotCalled(t, "Delete")
}

func TestCreatePhotos_NoPhotos(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	_, err := newService(repo, store).CreatePhotos(context.Background(), nil)

	assertValidationError(t, err, "at least one photo is required")
	repo.AssertNotCalled(t, "CreatePhotos")
}

func TestCreatePhotos_DBFailureCleansUpFiles(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	store.On("Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()
	repo.On("CreatePhotos", mock.Anything, mock.Anything).Return(assert.AnError).Once()
	store.On("Delete", mock.Anything, mock.Anything).Return(nil).Twice()

	_, err := newService(repo, store).CreatePhotos(context.Background(), []string{pngB64("a"), pngB64("b")})

	require.Error(t, err)
	repo.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestDeletePhoto_Standalone(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("GetPhotoByToken", mock.Anything, "tok").
		Return(&domain.Photo{ID: 7, Token: "tok", StoredKey: "photos/tok.png"}, nil)
	repo.On("DeletePhoto", mock.Anything, int64(7)).Return(nil).Once()
	store.On("Delete", mock.Anything, "photos/tok.png").Return(nil).Once()

	err := newService(repo, store).DeletePhoto(context.Background(), "tok")

	require.NoError(t, err)
	repo.AssertNotCalled(t, "CountPhotosForSnap")
	repo.AssertNotCalled(t, "DeleteSnap")
	repo.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestDeletePhoto_LastPhotoDeletesSnap(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	snapID := int64(5)
	repo.On("GetPhotoByToken", mock.Anything, "tok").
		Return(&domain.Photo{ID: 7, Token: "tok", StoredKey: "photos/tok.png", SnapID: &snapID}, nil)
	repo.On("DeletePhoto", mock.Anything, int64(7)).Return(nil).Once()
	store.On("Delete", mock.Anything, "photos/tok.png").Return(nil).Once()
	repo.On("CountPhotosForSnap", mock.Anything, int64(5)).Return(0, nil).Once()
	repo.On("DeleteSnap", mock.Anything, int64(5)).Return(nil).Once()

	err := newService(repo, store).DeletePhoto(context.Background(), "tok")

	require.NoError(t, err)
	repo.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestDeletePhoto_SnapWithRemainingPhotosKept(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	snapID := int64(5)
	repo.On("GetPhotoByToken", mock.Anything, "tok").
		Return(&domain.Photo{ID: 7, Token: "tok", StoredKey: "photos/tok.png", SnapID: &snapID}, nil)
	repo.On("DeletePhoto", mock.Anything, int64(7)).Return(nil).Once()
	store.On("Delete", mock.Anything, "photos/tok.png").Return(nil).Once()
	repo.On("CountPhotosForSnap", mock.Anything, int64(5)).Return(2, nil).Once()

	err := newService(repo, store).DeletePhoto(context.Background(), "tok")

	require.NoError(t, err)
	repo.AssertNotCalled(t, "DeleteSnap")
	repo.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestDeletePhoto_NotFoundPassesThrough(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("GetPhotoByToken", mock.Anything, "nope").Return(nil, domain.ErrNotFound)

	err := newService(repo, store).DeletePhoto(context.Background(), "nope")

	assert.ErrorIs(t, err, domain.ErrNotFound)
	repo.AssertNotCalled(t, "DeletePhoto")
	store.AssertNotCalled(t, "Delete")
}

func TestGetPhoto_IncrementsViewCount(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("GetPhotoByToken", mock.Anything, "abc").
		Return(&domain.Photo{Token: "abc", StoredKey: "photos/abc.jpg"}, nil)
	store.On("Get", mock.Anything, "photos/abc.jpg").
		Return(io.NopCloser(strings.NewReader("x")), "image/jpeg", nil)
	repo.On("IncrementPhotoViews", mock.Anything, "abc").Return(nil).Once()

	rc, _, err := newService(repo, store).GetPhoto(context.Background(), "abc")
	require.NoError(t, err)
	_ = rc.Close()

	repo.AssertExpectations(t)
}

func TestListAllPhotos_PopulatesURLs(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("ListAllPhotos", mock.Anything).Return([]domain.Photo{
		{ID: 2, Token: "t2"}, {ID: 1, Token: "t1"},
	}, nil)

	photos, err := newService(repo, store).ListAllPhotos(context.Background())

	require.NoError(t, err)
	require.Len(t, photos, 2)
	assert.Equal(t, "/photos/t2", photos[0].URL)
	assert.Equal(t, "/photos/t1", photos[1].URL)
}

func TestListSnaps_PopulatesURLs(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("ListSnaps", mock.Anything).Return([]domain.Snap{
		{ID: 1, Photos: []domain.Photo{{Token: "t1"}, {Token: "t2"}}},
	}, nil)

	snaps, err := newService(repo, store).ListSnaps(context.Background())

	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, "/photos/t1", snaps[0].Photos[0].URL)
	assert.Equal(t, "/photos/t2", snaps[0].Photos[1].URL)
}

// realPNG encodes a w×h opaque PNG that the standard library can decode,
// used to exercise real thumbnail generation through the service.
func realPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestGetPhotoThumb_ServesStoredWhenPresent(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	thumb := "thumbs/tok.jpg"
	repo.On("GetPhotoByToken", mock.Anything, "tok").
		Return(&domain.Photo{ID: 7, Token: "tok", StoredKey: "photos/tok.png", ThumbKey: &thumb}, nil)
	store.On("Get", mock.Anything, thumb).
		Return(io.NopCloser(strings.NewReader("stored-thumb")), "image/jpeg", nil).Once()

	rc, err := newService(repo, store).GetPhotoThumb(context.Background(), "tok")

	require.NoError(t, err)
	got, _ := io.ReadAll(rc)
	require.NoError(t, rc.Close())
	assert.Equal(t, "stored-thumb", string(got))
	// A present thumbnail must not be regenerated or re-persisted.
	store.AssertNotCalled(t, "Put")
	repo.AssertNotCalled(t, "SetPhotoThumbKey")
	store.AssertExpectations(t)
}

func TestGetPhotoThumb_GeneratesAndPersistsOnMiss(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("GetPhotoByToken", mock.Anything, "tok").
		Return(&domain.Photo{ID: 7, Token: "tok", StoredKey: "photos/tok.png"}, nil)
	store.On("Get", mock.Anything, "photos/tok.png").
		Return(io.NopCloser(bytes.NewReader(realPNG(t, 500, 400))), "image/png", nil).Once()

	var stored []byte
	store.On("Put", mock.Anything, "thumbs/tok.jpg", mock.Anything, "image/jpeg").
		Run(func(args mock.Arguments) {
			stored, _ = io.ReadAll(args.Get(2).(io.Reader))
		}).Return(nil).Once()
	repo.On("SetPhotoThumbKey", mock.Anything, int64(7), "thumbs/tok.jpg").Return(nil).Once()

	rc, err := newService(repo, store).GetPhotoThumb(context.Background(), "tok")
	require.NoError(t, err)
	served, _ := io.ReadAll(rc)
	require.NoError(t, rc.Close())

	// The bytes served match the bytes persisted, and decode as a JPEG no
	// larger than the thumbnail cap.
	assert.Equal(t, stored, served)
	img, format, err := image.Decode(bytes.NewReader(served))
	require.NoError(t, err)
	assert.Equal(t, "jpeg", format)
	assert.LessOrEqual(t, img.Bounds().Dx(), 400)
	assert.LessOrEqual(t, img.Bounds().Dy(), 400)

	repo.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestGetPhotoThumb_RegeneratesWhenStoredUnreadable(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	thumb := "thumbs/tok.jpg"
	repo.On("GetPhotoByToken", mock.Anything, "tok").
		Return(&domain.Photo{ID: 7, Token: "tok", StoredKey: "photos/tok.png", ThumbKey: &thumb}, nil)
	// Stored thumbnail is gone — the row points at a missing file.
	store.On("Get", mock.Anything, thumb).Return(nil, "", errors.New("no such file")).Once()
	// Fall back to the original and regenerate.
	store.On("Get", mock.Anything, "photos/tok.png").
		Return(io.NopCloser(bytes.NewReader(realPNG(t, 200, 200))), "image/png", nil).Once()
	store.On("Put", mock.Anything, "thumbs/tok.jpg", mock.Anything, "image/jpeg").Return(nil).Once()
	repo.On("SetPhotoThumbKey", mock.Anything, int64(7), "thumbs/tok.jpg").Return(nil).Once()

	rc, err := newService(repo, store).GetPhotoThumb(context.Background(), "tok")
	require.NoError(t, err)
	require.NoError(t, rc.Close())

	repo.AssertExpectations(t)
	store.AssertExpectations(t)
}

func TestGetPhotoThumb_NotFoundPassesThrough(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	repo.On("GetPhotoByToken", mock.Anything, "nope").Return(nil, domain.ErrNotFound)

	_, err := newService(repo, store).GetPhotoThumb(context.Background(), "nope")

	assert.ErrorIs(t, err, domain.ErrNotFound)
	store.AssertNotCalled(t, "Get")
}

func TestDeletePhoto_DeletesThumbnail(t *testing.T) {
	repo := &repoMock.SnapRepository{}
	store := &storageMock.StorageProvider{}

	thumb := "thumbs/tok.jpg"
	repo.On("GetPhotoByToken", mock.Anything, "tok").
		Return(&domain.Photo{ID: 7, Token: "tok", StoredKey: "photos/tok.png", ThumbKey: &thumb}, nil)
	repo.On("DeletePhoto", mock.Anything, int64(7)).Return(nil).Once()
	store.On("Delete", mock.Anything, "photos/tok.png").Return(nil).Once()
	store.On("Delete", mock.Anything, thumb).Return(nil).Once()

	err := newService(repo, store).DeletePhoto(context.Background(), "tok")

	require.NoError(t, err)
	repo.AssertExpectations(t)
	store.AssertExpectations(t)
}
