package assetlogo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetAsset(ctx context.Context, id uuid.UUID) (*Asset, error) {
	query := `
		SELECT id, canonical_key, symbol, name, asset_class, metadata, created_at, updated_at
		FROM assets WHERE id = $1`
	a := &Asset{}
	var name *string
	var metadata []byte
	err := r.db.QueryRow(ctx, query, id).Scan(
		&a.ID, &a.CanonicalKey, &a.Symbol, &name, &a.AssetClass, &metadata, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	a.Name = name
	if len(metadata) > 0 {
		a.Metadata = json.RawMessage(metadata)
	} else {
		a.Metadata = json.RawMessage("{}")
	}
	return a, nil
}

func (r *Repository) GetAssetByCanonicalKey(ctx context.Context, key string) (*Asset, error) {
	query := `
		SELECT id, canonical_key, symbol, name, asset_class, metadata, created_at, updated_at
		FROM assets WHERE canonical_key = $1`
	a := &Asset{}
	var name *string
	var metadata []byte
	err := r.db.QueryRow(ctx, query, key).Scan(
		&a.ID, &a.CanonicalKey, &a.Symbol, &name, &a.AssetClass, &metadata, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	a.Name = name
	if len(metadata) > 0 {
		a.Metadata = json.RawMessage(metadata)
	} else {
		a.Metadata = json.RawMessage("{}")
	}
	return a, nil
}

func (r *Repository) CreateAsset(ctx context.Context, asset *Asset) error {
	if asset.ID == uuid.Nil {
		asset.ID = uuid.New()
	}
	if len(asset.Metadata) == 0 {
		asset.Metadata = json.RawMessage("{}")
	}
	var name interface{}
	if asset.Name != nil {
		name = *asset.Name
	}
	query := `
		INSERT INTO assets (id, canonical_key, symbol, name, asset_class, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (canonical_key) DO UPDATE SET
			symbol = EXCLUDED.symbol,
			name = EXCLUDED.name,
			asset_class = EXCLUDED.asset_class,
			metadata = EXCLUDED.metadata,
			updated_at = NOW()
		RETURNING id, created_at, updated_at`
	return r.db.QueryRow(ctx, query,
		asset.ID, asset.CanonicalKey, asset.Symbol, name, asset.AssetClass, []byte(asset.Metadata),
	).Scan(&asset.ID, &asset.CreatedAt, &asset.UpdatedAt)
}

func (r *Repository) GetAlias(ctx context.Context, venueID uuid.UUID, venueSymbol string) (*AssetAlias, error) {
	query := `
		SELECT id, asset_id, venue_id, venue_symbol, created_at
		FROM asset_aliases WHERE venue_id = $1 AND venue_symbol = $2`
	alias := &AssetAlias{}
	var vid *uuid.UUID
	err := r.db.QueryRow(ctx, query, venueID, venueSymbol).Scan(
		&alias.ID, &alias.AssetID, &vid, &alias.VenueSymbol, &alias.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	alias.VenueID = vid
	return alias, nil
}

func (r *Repository) CreateAlias(ctx context.Context, alias *AssetAlias) error {
	if alias.ID == uuid.Nil {
		alias.ID = uuid.New()
	}
	var vid interface{}
	if alias.VenueID != nil {
		vid = *alias.VenueID
	}
	query := `
		INSERT INTO asset_aliases (id, asset_id, venue_id, venue_symbol)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at`
	return r.db.QueryRow(ctx, query, alias.ID, alias.AssetID, vid, alias.VenueSymbol).Scan(&alias.CreatedAt)
}

func (r *Repository) GetLogo(ctx context.Context, assetID uuid.UUID, provider string) (*AssetLogo, error) {
	query := `
		SELECT id, asset_id, provider, status, url, storage_path, content_type, etag,
			size_bytes, fetched_at, retry_after, error, created_at, updated_at
		FROM asset_logos WHERE asset_id = $1 AND provider = $2`
	l := &AssetLogo{}
	var url, storagePath, contentType, etag, errStr *string
	var sizeBytes *int64
	var fetchedAt, retryAfter *time.Time
	err := r.db.QueryRow(ctx, query, assetID, provider).Scan(
		&l.ID, &l.AssetID, &l.Provider, &l.Status, &url, &storagePath, &contentType, &etag,
		&sizeBytes, &fetchedAt, &retryAfter, &errStr, &l.CreatedAt, &l.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	l.URL = url
	l.StoragePath = storagePath
	l.ContentType = contentType
	l.ETag = etag
	l.SizeBytes = sizeBytes
	l.FetchedAt = fetchedAt
	l.RetryAfter = retryAfter
	l.Error = errStr
	return l, nil
}

func (r *Repository) UpsertLogo(ctx context.Context, logo *AssetLogo) error {
	if logo.ID == uuid.Nil {
		logo.ID = uuid.New()
	}
	var url, storagePath, contentType, etag, errStr interface{}
	if logo.URL != nil {
		url = *logo.URL
	}
	if logo.StoragePath != nil {
		storagePath = *logo.StoragePath
	}
	if logo.ContentType != nil {
		contentType = *logo.ContentType
	}
	if logo.ETag != nil {
		etag = *logo.ETag
	}
	if logo.Error != nil {
		errStr = *logo.Error
	}
	var fetchedAt, retryAfter interface{}
	if logo.FetchedAt != nil {
		fetchedAt = *logo.FetchedAt
	}
	if logo.RetryAfter != nil {
		retryAfter = *logo.RetryAfter
	}
	query := `
		INSERT INTO asset_logos (id, asset_id, provider, status, url, storage_path, content_type, etag,
			size_bytes, fetched_at, retry_after, error)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (asset_id, provider) DO UPDATE SET
			status = EXCLUDED.status,
			url = EXCLUDED.url,
			storage_path = EXCLUDED.storage_path,
			content_type = EXCLUDED.content_type,
			etag = EXCLUDED.etag,
			size_bytes = EXCLUDED.size_bytes,
			fetched_at = EXCLUDED.fetched_at,
			retry_after = EXCLUDED.retry_after,
			error = EXCLUDED.error,
			updated_at = NOW()
		RETURNING created_at, updated_at`
	return r.db.QueryRow(ctx, query,
		logo.ID, logo.AssetID, logo.Provider, logo.Status, url, storagePath, contentType, etag,
		logo.SizeBytes, fetchedAt, retryAfter, errStr,
	).Scan(&logo.CreatedAt, &logo.UpdatedAt)
}

func (r *Repository) DeleteLogo(ctx context.Context, assetID uuid.UUID, provider string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM asset_logos WHERE asset_id = $1 AND provider = $2`, assetID, provider)
	return err
}

func (r *Repository) ListMissingAssets(ctx context.Context, limit int) ([]*Asset, error) {
	query := `
		SELECT DISTINCT a.id, a.canonical_key, a.symbol, a.name, a.asset_class, a.metadata, a.created_at, a.updated_at
		FROM assets a
		JOIN asset_logos l ON l.asset_id = a.id
		WHERE l.status IN ('missing','error')
		ORDER BY a.created_at DESC
		LIMIT $1`
	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Asset
	for rows.Next() {
		a := &Asset{}
		var name *string
		var metadata []byte
		if err := rows.Scan(&a.ID, &a.CanonicalKey, &a.Symbol, &name, &a.AssetClass, &metadata, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		a.Name = name
		if len(metadata) > 0 {
			a.Metadata = json.RawMessage(metadata)
		} else {
			a.Metadata = json.RawMessage("{}")
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repository) UpsertProviderRef(ctx context.Context, ref *AssetProviderRef) error {
	if ref.ID == uuid.Nil {
		ref.ID = uuid.New()
	}
	if len(ref.Ref) == 0 {
		ref.Ref = json.RawMessage("{}")
	}
	query := `
		INSERT INTO asset_provider_refs (id, asset_id, provider, ref)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (asset_id, provider) DO UPDATE SET
			ref = EXCLUDED.ref,
			updated_at = NOW()
		RETURNING created_at, updated_at`
	return r.db.QueryRow(ctx, query, ref.ID, ref.AssetID, ref.Provider, []byte(ref.Ref)).Scan(&ref.CreatedAt, &ref.UpdatedAt)
}

func (r *Repository) CreateProviderRef(ctx context.Context, ref *AssetProviderRef) error {
	return r.UpsertProviderRef(ctx, ref)
}

func (r *Repository) GetProviderRef(ctx context.Context, assetID uuid.UUID, provider string) (*AssetProviderRef, error) {
	query := `
		SELECT id, asset_id, provider, ref, created_at, updated_at
		FROM asset_provider_refs WHERE asset_id = $1 AND provider = $2`
	ref := &AssetProviderRef{}
	var raw []byte
	err := r.db.QueryRow(ctx, query, assetID, provider).Scan(
		&ref.ID, &ref.AssetID, &ref.Provider, &raw, &ref.CreatedAt, &ref.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		ref.Ref = json.RawMessage(raw)
	} else {
		ref.Ref = json.RawMessage("{}")
	}
	return ref, nil
}

func (r *Repository) ListProviderRefs(ctx context.Context, assetID uuid.UUID) ([]*AssetProviderRef, error) {
	query := `
		SELECT id, asset_id, provider, ref, created_at, updated_at
		FROM asset_provider_refs WHERE asset_id = $1`
	rows, err := r.db.Query(ctx, query, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AssetProviderRef
	for rows.Next() {
		ref := &AssetProviderRef{}
		var raw []byte
		if err := rows.Scan(&ref.ID, &ref.AssetID, &ref.Provider, &raw, &ref.CreatedAt, &ref.UpdatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			ref.Ref = json.RawMessage(raw)
		} else {
			ref.Ref = json.RawMessage("{}")
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

func (r *Repository) ListAssets(ctx context.Context) ([]*Asset, error) {
	query := `
		SELECT id, canonical_key, symbol, name, asset_class, metadata, created_at, updated_at
		FROM assets ORDER BY created_at DESC`
	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Asset
	for rows.Next() {
		a := &Asset{}
		var name *string
		var metadata []byte
		if err := rows.Scan(&a.ID, &a.CanonicalKey, &a.Symbol, &name, &a.AssetClass, &metadata, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		a.Name = name
		if len(metadata) > 0 {
			a.Metadata = json.RawMessage(metadata)
		} else {
			a.Metadata = json.RawMessage("{}")
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repository) ListLogosByStatus(ctx context.Context, status string, limit, offset int) ([]*AssetLogo, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	query := `
		SELECT id, asset_id, provider, status, url, storage_path, content_type, etag,
			size_bytes, fetched_at, retry_after, error, created_at, updated_at
		FROM asset_logos WHERE status = $1
		ORDER BY updated_at DESC
		LIMIT $2 OFFSET $3`
	rows, err := r.db.Query(ctx, query, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AssetLogo
	for rows.Next() {
		l := &AssetLogo{}
		var url, storagePath, contentType, etag, errStr *string
		var sizeBytes *int64
		var fetchedAt, retryAfter *time.Time
		if err := rows.Scan(&l.ID, &l.AssetID, &l.Provider, &l.Status, &url, &storagePath, &contentType, &etag,
			&sizeBytes, &fetchedAt, &retryAfter, &errStr, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		l.URL = url
		l.StoragePath = storagePath
		l.ContentType = contentType
		l.ETag = etag
		l.SizeBytes = sizeBytes
		l.FetchedAt = fetchedAt
		l.RetryAfter = retryAfter
		l.Error = errStr
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *Repository) ListStoragePaths(ctx context.Context) ([]string, error) {
	rows, err := r.db.Query(ctx, `SELECT DISTINCT storage_path FROM asset_logos WHERE storage_path IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p *string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		if p != nil && *p != "" {
			out = append(out, *p)
		}
	}
	return out, rows.Err()
}

func (r *Repository) ListAssetsByVenue(ctx context.Context, venueID uuid.UUID) ([]*Asset, error) {
	query := `
		SELECT DISTINCT a.id, a.canonical_key, a.symbol, a.name, a.asset_class, a.metadata, a.created_at, a.updated_at
		FROM assets a
		JOIN asset_aliases al ON al.asset_id = a.id
		WHERE al.venue_id = $1
		ORDER BY a.symbol`
	rows, err := r.db.Query(ctx, query, venueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Asset
	for rows.Next() {
		a := &Asset{}
		var name *string
		var metadata []byte
		if err := rows.Scan(&a.ID, &a.CanonicalKey, &a.Symbol, &name, &a.AssetClass, &metadata, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		a.Name = name
		if len(metadata) > 0 {
			a.Metadata = json.RawMessage(metadata)
		} else {
			a.Metadata = json.RawMessage("{}")
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
