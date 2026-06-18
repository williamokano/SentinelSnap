package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/williamokano/sentinelsnap/internal/domain"
)

type snapRepository struct {
	db *sqlx.DB
}

func New(db *sqlx.DB) *snapRepository {
	return &snapRepository{db: db}
}

// CreateSnapWithPhotos inserts the snap and all of its photos atomically:
// either every row lands or none do, so a mid-creation failure can never
// leave a snap without its photo metadata. Generated IDs are written back
// onto snap and the photos slice elements.
func (r *snapRepository) CreateSnapWithPhotos(ctx context.Context, snap *domain.Snap, photos []domain.Photo) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("create snap with photos: begin tx: %w", err)
	}
	// Rollback after a successful Commit is a harmless no-op.
	defer func() { _ = tx.Rollback() }()

	if err := tx.QueryRowContext(ctx,
		`INSERT INTO snaps (latitude, longitude) VALUES ($1, $2) RETURNING id, created_at`,
		snap.Latitude, snap.Longitude,
	).Scan(&snap.ID, &snap.CreatedAt); err != nil {
		return fmt.Errorf("create snap with photos: insert snap: %w", err)
	}

	for i := range photos {
		photos[i].SnapID = &snap.ID
		if err := tx.QueryRowContext(ctx,
			`INSERT INTO photos (snap_id, stored_key, token) VALUES ($1, $2, $3) RETURNING id, created_at`,
			snap.ID, photos[i].StoredKey, photos[i].Token,
		).Scan(&photos[i].ID, &photos[i].CreatedAt); err != nil {
			return fmt.Errorf("create snap with photos: insert photo %d: %w", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("create snap with photos: commit: %w", err)
	}
	return nil
}

func (r *snapRepository) DeleteSnap(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM snaps WHERE id = $1`, id)
	return err
}

func (r *snapRepository) UpdateSnapName(ctx context.Context, id int64, name string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE snaps SET name = $2 WHERE id = $1`, id, name)
	if err != nil {
		return fmt.Errorf("update snap name: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *snapRepository) ListSnaps(ctx context.Context) ([]domain.Snap, error) {
	var snaps []domain.Snap
	if err := r.db.SelectContext(ctx, &snaps,
		`SELECT id, COALESCE(name, '') AS name, latitude, longitude, created_at FROM snaps ORDER BY created_at DESC`,
	); err != nil {
		return nil, fmt.Errorf("list snaps: %w", err)
	}
	if len(snaps) == 0 {
		return snaps, nil
	}

	ids := make([]int64, len(snaps))
	for i, s := range snaps {
		ids[i] = s.ID
	}

	query, args, err := sqlx.In(`SELECT id, snap_id, stored_key, token, view_count, created_at FROM photos WHERE snap_id IN (?) ORDER BY snap_id, id`, ids)
	if err != nil {
		return nil, fmt.Errorf("build photos query: %w", err)
	}
	query = r.db.Rebind(query)

	var photos []domain.Photo
	if err := r.db.SelectContext(ctx, &photos, query, args...); err != nil {
		return nil, fmt.Errorf("list photos: %w", err)
	}

	photosBySnap := make(map[int64][]domain.Photo, len(snaps))
	for _, p := range photos {
		// snap_id is guaranteed non-NULL here: the query filters on snap_id IN (...).
		if p.SnapID == nil {
			continue
		}
		photosBySnap[*p.SnapID] = append(photosBySnap[*p.SnapID], p)
	}
	for i := range snaps {
		snaps[i].Photos = photosBySnap[snaps[i].ID]
		if snaps[i].Photos == nil {
			snaps[i].Photos = []domain.Photo{}
		}
	}

	return snaps, nil
}

func (r *snapRepository) GetSnapByID(ctx context.Context, id int64) (*domain.Snap, error) {
	var snap domain.Snap
	err := r.db.GetContext(ctx, &snap,
		`SELECT id, COALESCE(name, '') AS name, latitude, longitude, created_at FROM snaps WHERE id = $1`, id,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get snap: %w", err)
	}
	photos, err := r.ListPhotosForSnap(ctx, id)
	if err != nil {
		return nil, err
	}
	snap.Photos = photos
	return &snap, nil
}

func (r *snapRepository) GetPhotoByToken(ctx context.Context, token string) (*domain.Photo, error) {
	var photo domain.Photo
	err := r.db.GetContext(ctx, &photo,
		`SELECT id, snap_id, stored_key, token, view_count, created_at FROM photos WHERE token = $1`, token,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get photo by token: %w", err)
	}
	return &photo, nil
}

func (r *snapRepository) ListPhotosForSnap(ctx context.Context, snapID int64) ([]domain.Photo, error) {
	var photos []domain.Photo
	if err := r.db.SelectContext(ctx, &photos,
		`SELECT id, snap_id, stored_key, token, view_count, created_at FROM photos WHERE snap_id = $1 ORDER BY id`, snapID,
	); err != nil {
		return nil, fmt.Errorf("list photos for snap %d: %w", snapID, err)
	}
	return photos, nil
}

// CreatePhotos inserts standalone photos (snap_id NULL) in one transaction,
// writing the generated IDs back onto the passed structs.
func (r *snapRepository) CreatePhotos(ctx context.Context, photos []domain.Photo) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("create photos: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for i := range photos {
		if err := tx.QueryRowContext(ctx,
			`INSERT INTO photos (snap_id, stored_key, token) VALUES (NULL, $1, $2) RETURNING id, created_at`,
			photos[i].StoredKey, photos[i].Token,
		).Scan(&photos[i].ID, &photos[i].CreatedAt); err != nil {
			return fmt.Errorf("create photos: insert photo %d: %w", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("create photos: commit: %w", err)
	}
	return nil
}

func (r *snapRepository) ListAllPhotos(ctx context.Context) ([]domain.Photo, error) {
	var photos []domain.Photo
	if err := r.db.SelectContext(ctx, &photos,
		`SELECT id, snap_id, stored_key, token, view_count, created_at FROM photos ORDER BY created_at DESC, id DESC`,
	); err != nil {
		return nil, fmt.Errorf("list all photos: %w", err)
	}
	if photos == nil {
		photos = []domain.Photo{}
	}
	return photos, nil
}

func (r *snapRepository) DeletePhoto(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM photos WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete photo %d: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *snapRepository) CountPhotosForSnap(ctx context.Context, snapID int64) (int, error) {
	var n int
	if err := r.db.GetContext(ctx, &n,
		`SELECT COUNT(*) FROM photos WHERE snap_id = $1`, snapID,
	); err != nil {
		return 0, fmt.Errorf("count photos for snap %d: %w", snapID, err)
	}
	return n, nil
}

func (r *snapRepository) IncrementPhotoViews(ctx context.Context, token string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE photos SET view_count = view_count + 1 WHERE token = $1`, token,
	)
	if err != nil {
		return fmt.Errorf("increment photo views: %w", err)
	}
	return nil
}
