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
	AddFavorite(
		ctx context.Context,
		parentAccountID string,
		packageID string,
		now time.Time,
	) (domain.Favorite, error)
	RemoveFavorite(
		ctx context.Context,
		parentAccountID string,
		packageID string,
	) (bool, error)
	ListFavorites(
		ctx context.Context,
		parentAccountID string,
	) ([]domain.FavoriteView, error)
	RecordPlayback(
		ctx context.Context,
		input domain.PlayRecordInput,
		limit int,
		now time.Time,
	) (domain.PlayRecord, error)
	ListPlayHistory(
		ctx context.Context,
		query domain.PlayHistoryQuery,
	) ([]domain.PlayRecord, error)
	ListThemePacks(
		ctx context.Context,
		query domain.ThemePackQuery,
	) ([]domain.ThemePack, error)
	GetThemePack(ctx context.Context, themeID string) (*domain.ThemePack, error)
	// UpsertThemePack creates or replaces a draft theme pack. The service
	// validates members before calling, so a non-draft pack is never replaced.
	UpsertThemePack(
		ctx context.Context,
		input domain.ThemePackInput,
		now time.Time,
	) (*domain.ThemePack, error)
	// TransitionThemePack publishes or withdraws a pack and stamps the current
	// catalog revision on publish. It returns ErrThemePackNotFound when the id
	// is unknown and the transition error when the move is illegal.
	TransitionThemePack(
		ctx context.Context,
		themeID string,
		action domain.Action,
		now time.Time,
	) (*domain.ThemePack, error)
	// DeleteThemePack removes a draft pack. A published pack must be withdrawn
	// first, so an operator cannot silently drop a deliverable theme.
	DeleteThemePack(ctx context.Context, themeID string) error
}
