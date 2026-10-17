// Package service contains content recommendation use cases.
//
// The recommender is deliberately explainable and bounded: it ranks only
// published catalog candidates, boosts packages that share a category with the
// family's favourites or recent plays, matches the child's age tier, and drops
// anything already favourited, recently played, or explicitly excluded. The
// caller passes the authenticated family id and the service never trusts a
// client-provided owner.
package service

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/recommendation/domain"
)

// DefaultLimit and MaxLimit bound a recommendation read so a long-lived family
// cannot force an unbounded response.
const (
	DefaultLimit = 8
	MaxLimit     = 24
)

// CatalogReader returns the published catalog candidates the recommender may
// rank. It must only return packages with a currently published version.
type CatalogReader interface {
	ListCandidates(ctx context.Context, familyID string, ageTier string) ([]domain.Candidate, error)
}

// FamilySignalReader reads the family's favourites and recent plays, which the
// recommender uses to boost similar categories and exclude what is already
// consumed.
type FamilySignalReader interface {
	// FavoritePackageIDs returns the packages the family favourited.
	FavoritePackageIDs(ctx context.Context, familyID string) ([]string, error)
	// FavoriteCategories returns the categories of those favourites.
	FavoriteCategories(ctx context.Context, familyID string) ([]string, error)
	// RecentPackageIDs returns the packages the family played recently,
	// newest first.
	RecentPackageIDs(ctx context.Context, familyID string, limit int) ([]string, error)
	// RecentCategories returns the categories of those recent plays.
	RecentCategories(ctx context.Context, familyID string, limit int) ([]string, error)
}

// Service owns recommendation ranking.
type Service struct {
	catalog CatalogReader
	signals FamilySignalReader
}

// Options contains recommendation-service dependencies.
type Options struct {
	Catalog CatalogReader
	Signals FamilySignalReader
}

// New creates the recommendation service.
func New(options Options) *Service {
	return &Service{catalog: options.Catalog, signals: options.Signals}
}

// Recommend returns a bounded, ranked list of published candidates.
func (s *Service) Recommend(
	ctx context.Context,
	familyID string,
	query domain.Query,
) ([]domain.Recommendation, error) {
	normalized, err := domain.ValidateQuery(query)
	if err != nil {
		return nil, err
	}
	if s.catalog == nil {
		return []domain.Recommendation{}, nil
	}
	candidates, err := s.catalog.ListCandidates(ctx, familyID, normalized.AgeTier)
	if err != nil {
		return nil, err
	}
	excluded := make(map[string]struct{}, len(normalized.ExcludePackageIDs))
	for _, id := range normalized.ExcludePackageIDs {
		excluded[id] = struct{}{}
	}
	var favoriteIDs, recentIDs, favoriteCategories, recentCategories []string
	if s.signals != nil {
		if favoriteIDs, err = s.signals.FavoritePackageIDs(ctx, familyID); err != nil {
			return nil, err
		}
		if favoriteCategories, err = s.signals.FavoriteCategories(ctx, familyID); err != nil {
			return nil, err
		}
		if recentIDs, err = s.signals.RecentPackageIDs(ctx, familyID, MaxLimit); err != nil {
			return nil, err
		}
		if recentCategories, err = s.signals.RecentCategories(ctx, familyID, MaxLimit); err != nil {
			return nil, err
		}
	}
	for _, id := range append(append([]string{}, favoriteIDs...), recentIDs...) {
		excluded[id] = struct{}{}
	}
	favoriteCategorySet := toSet(favoriteCategories)
	recentCategorySet := toSet(recentCategories)

	ranked := make([]domain.Recommendation, 0, len(candidates))
	for _, candidate := range candidates {
		if _, skip := excluded[candidate.PackageID]; skip {
			continue
		}
		recommendation := domain.Recommendation{
			PackageID: candidate.PackageID,
			Title:     candidate.Title,
			Category:  candidate.Category,
			AgeTiers:  candidate.AgeTiers,
		}
		// Score is a small, explainable integer. A favourite-category match is
		// the strongest signal; a recent-category match is next; an age-tier
		// match keeps the suggestion suitable; a fresh title is a tiebreaker.
		score := 0
		reason := domain.ReasonFresh
		if _, ok := favoriteCategorySet[candidate.Category]; ok {
			score += 4
			reason = domain.ReasonSameCategory
		} else if _, ok := recentCategorySet[candidate.Category]; ok {
			score += 3
			reason = domain.ReasonSameCategory
		}
		if normalized.AgeTier != "" && contains(candidate.AgeTiers, normalized.AgeTier) {
			score += 2
			if reason == domain.ReasonFresh {
				reason = domain.ReasonSameAgeTier
			}
		}
		if !candidate.PublishedAt.IsZero() {
			// Only ever 0 or 1, so freshness never outranks a category match.
			score += 1
		}
		recommendation.Score = score
		recommendation.Reason = reason
		ranked = append(ranked, recommendation)
	}
	// Highest score first; ties keep a stable order by package id.
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].PackageID < ranked[j].PackageID
	})
	limit := normalized.Limit
	switch {
	case limit <= 0:
		limit = DefaultLimit
	case limit > MaxLimit:
		limit = MaxLimit
	}
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked, nil
}

// CheckReadiness verifies that the recommendation catalog is readable without
// requiring a family scope or playback history. An empty catalog is healthy:
// it is a valid first-deployment state, not a service failure.
func (s *Service) CheckReadiness(ctx context.Context) error {
	if s == nil || s.catalog == nil {
		return errors.New("recommendation catalog is unavailable")
	}
	_, err := s.catalog.ListCandidates(ctx, "", "")
	return err
}

func toSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			set[trimmed] = struct{}{}
		}
	}
	return set
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
