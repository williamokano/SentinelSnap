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

func (r *snapRepository) CreateSnap(ctx context.Context, snap *domain.Snap) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO snaps (latitude, longitude) VALUES ($1, $2) RETURNING id`,
		snap.Latitude, snap.Longitude,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create snap: %w", err)
	}
	return id, nil
}

func (r *snapRepository) AddPhoto(ctx context.Context, photo *domain.Photo) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO photos (snap_id, stored_key, token) VALUES ($1, $2, $3) RETURNING id`,
		photo.SnapID, photo.StoredKey, photo.Token,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("add photo: %w", err)
	}
	return id, nil
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

	query, args, err := sqlx.In(`SELECT id, snap_id, stored_key, token, created_at FROM photos WHERE snap_id IN (?) ORDER BY snap_id, id`, ids)
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
		photosBySnap[p.SnapID] = append(photosBySnap[p.SnapID], p)
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
		`SELECT id, snap_id, stored_key, token, created_at FROM photos WHERE token = $1`, token,
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
		`SELECT id, snap_id, stored_key, token, created_at FROM photos WHERE snap_id = $1 ORDER BY id`, snapID,
	); err != nil {
		return nil, fmt.Errorf("list photos for snap %d: %w", snapID, err)
	}
	return photos, nil
}
