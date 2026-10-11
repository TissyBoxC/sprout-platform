// Package repository persists remote UI text packages, their entries, and the
// published-catalog clock.
//
// Lifecycle writes are atomic: the package row, its entries, the withdrawal of
// a superseded publication, and the catalog revision bump commit together or
// none of them do.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/domain"
)

// ErrNotFound reports a missing package version.
var ErrNotFound = errors.New("ui text repository: not found")

// Repository defines persistence operations for remote UI text.
type Repository interface {
	// NextVersion allocates the next version number for one logical package.
	NextVersion(
		ctx context.Context,
		packageID string,
		language string,
		platform domain.Platform,
	) (int, error)
	// CreateVersion stores a new draft version and its entries atomically.
	CreateVersion(
		ctx context.Context,
		input domain.DraftInput,
		packageVersion int,
		actorID string,
		now time.Time,
	) (*domain.PackageDetail, error)
	// UpdateDraft replaces the entries and metadata of one draft version.
	UpdateDraft(
		ctx context.Context,
		input domain.DraftInput,
		packageVersion int,
		now time.Time,
	) (*domain.PackageDetail, error)
	// GetVersion returns one stored version with its ordered entries.
	GetVersion(
		ctx context.Context,
		packageID string,
		language string,
		platform domain.Platform,
		packageVersion int,
	) (*domain.PackageDetail, error)
	// ListVersions returns one operator page of version summaries plus the total.
	ListVersions(
		ctx context.Context,
		filter domain.VersionFilter,
	) ([]domain.PackageVersion, int, error)
	// ApplyTransition commits one lifecycle move. Publishing withdraws the
	// superseded publication, while publish and withdraw both advance the
	// catalog revision so clients receive additions and removals.
	ApplyTransition(
		ctx context.Context,
		transition domain.TransitionInput,
		now time.Time,
	) (*domain.Package, error)
	// CurrentRevision returns the published-catalog cursor without listing.
	CurrentRevision(ctx context.Context) (int64, error)
	// ListPublished returns published packages and withdrawn package keys newer
	// than one revision.
	ListPublished(
		ctx context.Context,
		sinceRevision int64,
		limit int,
	) (domain.RevisionPage, error)
	// PublishedPackage returns the currently published version of one package.
	PublishedPackage(
		ctx context.Context,
		packageID string,
		language string,
		platform domain.Platform,
	) (*domain.PublishedPackage, error)
}
