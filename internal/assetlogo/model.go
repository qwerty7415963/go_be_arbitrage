package assetlogo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type AssetClass string

const (
	AssetClassCryptoNative AssetClass = "crypto_native"
	AssetClassCryptoToken  AssetClass = "crypto_token"
	AssetClassIndex        AssetClass = "index"
	AssetClassCommodity    AssetClass = "commodity"
	AssetClassEquity       AssetClass = "equity"
	AssetClassUnknown      AssetClass = "unknown"
)

func (a AssetClass) Valid() bool {
	switch a {
	case AssetClassCryptoNative, AssetClassCryptoToken, AssetClassIndex,
		AssetClassCommodity, AssetClassEquity, AssetClassUnknown:
		return true
	}
	return false
}

type Asset struct {
	ID           uuid.UUID       `json:"id" db:"id"`
	CanonicalKey string          `json:"canonical_key" db:"canonical_key"`
	Symbol       string          `json:"symbol" db:"symbol"`
	Name         *string         `json:"name,omitempty" db:"name"`
	AssetClass   AssetClass      `json:"asset_class" db:"asset_class"`
	Metadata     json.RawMessage `json:"metadata" db:"metadata"`
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at" db:"updated_at"`
}

type AssetAlias struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	AssetID     uuid.UUID  `json:"asset_id" db:"asset_id"`
	VenueID     *uuid.UUID `json:"venue_id,omitempty" db:"venue_id"`
	VenueSymbol string     `json:"venue_symbol" db:"venue_symbol"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
}

type AssetProviderRef struct {
	ID        uuid.UUID       `json:"id" db:"id"`
	AssetID   uuid.UUID       `json:"asset_id" db:"asset_id"`
	Provider  string          `json:"provider" db:"provider"`
	Ref       json.RawMessage `json:"ref" db:"ref"`
	CreatedAt time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt time.Time       `json:"updated_at" db:"updated_at"`
}

type LogoStatus string

const (
	LogoStatusFound   LogoStatus = "found"
	LogoStatusMissing LogoStatus = "missing"
	LogoStatusError   LogoStatus = "error"
	LogoStatusPending LogoStatus = "pending"
)

type AssetLogo struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	AssetID     uuid.UUID  `json:"asset_id" db:"asset_id"`
	Provider    string     `json:"provider" db:"provider"`
	Status      LogoStatus `json:"status" db:"status"`
	URL         *string    `json:"url,omitempty" db:"url"`
	StoragePath *string    `json:"storage_path,omitempty" db:"storage_path"`
	ContentType *string    `json:"content_type,omitempty" db:"content_type"`
	ETag        *string    `json:"etag,omitempty" db:"etag"`
	SizeBytes   *int64     `json:"size_bytes,omitempty" db:"size_bytes"`
	FetchedAt   *time.Time `json:"fetched_at,omitempty" db:"fetched_at"`
	RetryAfter  *time.Time `json:"retry_after,omitempty" db:"retry_after"`
	Error       *string    `json:"error,omitempty" db:"error"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
}

type ResolveInput struct {
	Asset Asset
	Ref   AssetProviderRef
}

type ResolvedLogo struct {
	Provider    string
	URL         string
	ETag        string
	ContentType string
	Bytes       []byte
	RetryAfter  time.Duration
	StatusCode  int
}

type Provider interface {
	Name() string
	Resolve(ctx context.Context, asset Asset, ref AssetProviderRef) (ResolvedLogo, error)
}
