// Package repository persists content-package metadata and review history.
package repository

import (
	"context"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
)

// Repository is the persistence contract for content-package lifecycle data.
//
// Lifecycle writes must be atomic: the package row, the review log, and the
// catalog revision all commit together or none of them do.
type Repository interface {
	List(
		ctx context.Context,
		filter domain.PackageListFilter,
	) (domain.PackagePage, error)
	GetDetail(ctx context.Context, packageID string) (*domain.PackageDetail, error)
	GetVersion(
		ctx context.Context,
		packageID string,
		packageVersion int,
	) (*domain.PackageVersion, error)
	CreateDraft(
		ctx context.Context,
		input domain.MetadataInput,
		now time.Time,
	) (*domain.PackageVersion, error)
	CreateVersion(
		ctx context.Context,
		input domain.MetadataInput,
		packageVersion int,
		now time.Time,
	) (*domain.PackageVersion, error)
	UpdateDraft(
		ctx context.Context,
		packageID string,
		packageVersion int,
		input domain.MetadataInput,
		now time.Time,
	) (*domain.PackageVersion, error)
	ApplyTransition(
		ctx context.Context,
		transition domain.Transition,
	) (*domain.PackageVersion, error)
	Catalog(ctx context.Context, query domain.CatalogQuery) (*domain.Catalog, error)
	// CatalogRevision returns the current delivery cursor without listing
	// packages. The device path uses it when a family policy exposes no
	// categories, so the client still learns the server revision.
	CatalogRevision(ctx context.Context) (int64, error)
	PublishedDownload(
		ctx context.Context,
		packageID string,
	) (*domain.PackageVersion, error)
}
