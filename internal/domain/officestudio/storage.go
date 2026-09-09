package officestudio

import (
	"context"
	"errors"
	"time"
)

var ErrStorageQuota = errors.New("OFFICE_STORAGE_QUOTA")
var ErrBlobLease = errors.New("OFFICE_BLOB_LEASE_EXPIRED")

// StoragePolicy is application-wide configuration, never a model-supplied budget.
type StoragePolicy struct {
	MaxBytes int64
	Grace    time.Duration
	LeaseTTL time.Duration
}

func DefaultStoragePolicy() StoragePolicy {
	return StoragePolicy{MaxBytes: 2 << 30, Grace: 24 * time.Hour, LeaseTTL: 15 * time.Minute}
}

type BlobFile struct {
	Digest    string
	Name      string // inventory-only: non-blob files are counted, never registered for GC
	Size      int64
	StageOnly bool // cleanup of a completed/failed lease's registered stage, not its shared blob
}

type BlobReservation struct {
	TaskID    string
	Digest    string
	Size      int64
	MaxBytes  int64
	Now       time.Time
	ExpiresAt time.Time
}

type BlobLease struct {
	ID        string
	TaskID    string
	Digest    string
	Size      int64
	StageName string
	ExpiresAt time.Time
}

type StorageUsage struct {
	LimitBytes       int64 `json:"limitBytes"`
	UsedBytes        int64 `json:"usedBytes"`
	ReservedBytes    int64 `json:"reservedBytes"`
	TotalBytes       int64 `json:"totalBytes"`
	ReferencedBytes  int64 `json:"referencedBytes"`
	UnmanagedBytes   int64 `json:"unmanagedBytes"`
	BlobCount        int64 `json:"blobCount"`
	ActiveLeaseCount int64 `json:"activeLeaseCount"`
	OverLimit        bool  `json:"overLimit"`
}

type StorageSweepOptions struct {
	DryRun     bool   `json:"dryRun"`
	Limit      int    `json:"limit"`
	RequestKey string `json:"-"`
}

type StorageSweepReport struct {
	DryRun                   bool     `json:"dryRun"`
	Candidates               int      `json:"candidates"`
	Removed                  int      `json:"removed"`
	FreedBytes               int64    `json:"freedBytes"`
	ReleasedReservationBytes int64    `json:"releasedReservationBytes"`
	HasMore                  bool     `json:"hasMore"`
	Errors                   []string `json:"errors"`
}

// BlobStore serializes physical publication/deletion with SQLite writers.
// Callbacks only touch the service's managed directory and must not call Store.
// Global reference checks intentionally inspect every organization; no record
// content or foreign task identity is returned by these storage operations.
type BlobStore interface {
	ReconcileOfficeStorage(context.Context, func() ([]BlobFile, error)) error
	ReserveOfficeBlob(context.Context, BlobReservation) (BlobLease, error)
	WriteOfficeBlob(context.Context, string, time.Time, func(BlobLease) error) error
	RenewOfficeBlob(context.Context, string, time.Time, time.Time) error
	ReleaseOfficeBlob(context.Context, string, time.Time, func(BlobFile, []string) (int64, error)) error
	OfficeStorageUsage(context.Context, time.Time) (StorageUsage, error)
	SweepOfficeStorage(context.Context, StorageSweepOptions, time.Time, time.Duration, func(BlobFile, []string) (int64, error)) (StorageSweepReport, error)
}
