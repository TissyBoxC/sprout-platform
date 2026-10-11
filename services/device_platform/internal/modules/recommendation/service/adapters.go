package service

import (
	"context"
	"sort"
	"time"

	contentdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/domain"
	contentrepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/repository"
	bindingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/service"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/recommendation/domain"
)

// ContentCatalogReader adapts the content repository to the narrow
// CatalogReader the recommender depends on.
type ContentCatalogReader struct {
	Repository contentrepository.Repository
}

// ListCandidates returns published catalog packages for one age tier. An empty
// age tier returns every published package.
func (reader ContentCatalogReader) ListCandidates(
	ctx context.Context,
	ageTier string,
) ([]domain.Candidate, error) {
	if reader.Repository == nil {
		return nil, nil
	}
	catalog, err := reader.Repository.Catalog(ctx, contentdomain.CatalogQuery{
		AgeTier: ageTier,
	})
	if err != nil {
		return nil, err
	}
	candidates := make([]domain.Candidate, 0, len(catalog.Packages))
	for index := range catalog.Packages {
		pkg := catalog.Packages[index]
		ageTiers := make([]string, 0, len(pkg.AgeTiers))
		for _, tier := range pkg.AgeTiers {
			ageTiers = append(ageTiers, string(tier))
		}
		candidates = append(candidates, domain.Candidate{
			PackageID:   pkg.PackageID,
			Title:       pkg.Title,
			Category:    string(pkg.Category),
			AgeTiers:    ageTiers,
			PublishedAt: publishedAt(pkg),
		})
	}
	return candidates, nil
}

// publishedAt returns the publish time when present so freshness can act as a
// tiebreaker.
func publishedAt(pkg contentdomain.PackageVersion) time.Time {
	if pkg.PublishedAt != nil {
		return *pkg.PublishedAt
	}
	return time.Time{}
}

// ContentSignalReader adapts the content repository to the FamilySignalReader
// the recommender depends on. Favourites are keyed by the parent account id,
// which is the family scope the platform threads through the guardian path.
type ContentSignalReader struct {
	Repository contentrepository.Repository
	// Devices resolves the family's device ids so recent playback can be read
	// across every bound device.
	Devices DeviceReader
}

// FavoritePackageIDs returns the family's favourited package ids.
func (reader ContentSignalReader) FavoritePackageIDs(
	ctx context.Context,
	familyID string,
) ([]string, error) {
	if reader.Repository == nil {
		return nil, nil
	}
	favorites, err := reader.Repository.ListFavorites(ctx, familyID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(favorites))
	for _, favorite := range favorites {
		ids = append(ids, favorite.PackageID)
	}
	return ids, nil
}

// FavoriteCategories returns the categories of the family's favourites.
func (reader ContentSignalReader) FavoriteCategories(
	ctx context.Context,
	familyID string,
) ([]string, error) {
	if reader.Repository == nil {
		return nil, nil
	}
	favorites, err := reader.Repository.ListFavorites(ctx, familyID)
	if err != nil {
		return nil, err
	}
	categories := make([]string, 0, len(favorites))
	for _, favorite := range favorites {
		if favorite.Category != "" {
			categories = append(categories, string(favorite.Category))
		}
	}
	return categories, nil
}

// recentRecords returns the family's recent playback, newest first, merged
// across every bound device.
func (reader ContentSignalReader) recentRecords(
	ctx context.Context,
	familyID string,
) ([]contentdomain.PlayRecord, error) {
	if reader.Repository == nil || reader.Devices == nil {
		return nil, nil
	}
	deviceIDs, err := reader.Devices.FamilyDeviceIDs(ctx, familyID)
	if err != nil {
		return nil, err
	}
	records := make([]contentdomain.PlayRecord, 0, 32)
	for _, deviceID := range deviceIDs {
		deviceRecords, err := reader.Repository.ListPlayHistory(
			ctx,
			contentdomain.PlayHistoryQuery{DeviceID: deviceID, Limit: MaxLimit},
		)
		if err != nil {
			return nil, err
		}
		records = append(records, deviceRecords...)
	}
	// Newest first across devices so the recommender sees a consistent order.
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].PlayedAt.After(records[j].PlayedAt)
	})
	return records, nil
}

// RecentPackageIDs returns the recently played package ids, newest first.
func (reader ContentSignalReader) RecentPackageIDs(
	ctx context.Context,
	familyID string,
	limit int,
) ([]string, error) {
	records, err := reader.recentRecords(ctx, familyID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if _, exists := seen[record.PackageID]; exists {
			continue
		}
		seen[record.PackageID] = struct{}{}
		ids = append(ids, record.PackageID)
		if limit > 0 && len(ids) >= limit {
			break
		}
	}
	return ids, nil
}

// RecentCategories returns the categories of the recent plays, newest first.
func (reader ContentSignalReader) RecentCategories(
	ctx context.Context,
	familyID string,
	limit int,
) ([]string, error) {
	ids, err := reader.RecentPackageIDs(ctx, familyID, limit)
	if err != nil {
		return nil, err
	}
	// The playback history rows do not carry a category, so resolve it from
	// the published catalog per id. This keeps the signal honest: a category is
	// only used when the package still has a published version.
	categories := make([]string, 0, len(ids))
	for _, packageID := range ids {
		published, err := reader.Repository.PublishedDownload(ctx, packageID)
		if err != nil {
			continue
		}
		categories = append(categories, string(published.Category))
	}
	return categories, nil
}

// DeviceReader resolves the device ids bound to one family.
type DeviceReader interface {
	FamilyDeviceIDs(ctx context.Context, familyID string) ([]string, error)
}

// BindingDeviceReader adapts the device-binding service to the DeviceReader
// interface the recommender uses to resolve a family's recent playback.
type BindingDeviceReader struct {
	Service *bindingservice.Service
}

// FamilyDeviceIDs returns the device ids bound to one family.
func (reader BindingDeviceReader) FamilyDeviceIDs(
	ctx context.Context,
	familyID string,
) ([]string, error) {
	if reader.Service == nil {
		return nil, nil
	}
	bindings, err := reader.Service.List(ctx, familyID)
	if err != nil {
		return nil, err
	}
	deviceIDs := make([]string, 0, len(bindings))
	for index := range bindings {
		deviceIDs = append(deviceIDs, bindings[index].DeviceID)
	}
	return deviceIDs, nil
}
