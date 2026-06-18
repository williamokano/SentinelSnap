package domain

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")

type Snap struct {
	ID        int64     `db:"id"         json:"id"`
	Name      string    `db:"name"       json:"name"`
	Latitude  float64   `db:"latitude"   json:"latitude"`
	Longitude float64   `db:"longitude"  json:"longitude"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	Photos    []Photo   `db:"-"          json:"photos"`
}

type Photo struct {
	ID        int64     `db:"id"         json:"id"`
	SnapID    *int64    `db:"snap_id"    json:"snap_id,omitempty"`
	Token     string    `db:"token"      json:"-"`
	URL       string    `db:"-"          json:"url"`
	StoredKey string    `db:"stored_key" json:"-"`
	ThumbKey  *string   `db:"thumb_key"  json:"-"`
	ViewCount int64     `db:"view_count" json:"-"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}
